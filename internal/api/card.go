package api

import (
	"net/http"

	"im-/internal/callback"
	"im-/internal/core"
	"im-/internal/store"
)

// RegisterCardAPI 挂载模板卡片交互相关接口（M3）：
//   - POST /api/card/interact                  客户端：用户点击卡片按钮/选项 → 卡片交互事件回调
//   - POST /cgi-bin/aibot/update_template_card 接入方：用事件携带的 response_code 原地更新原卡片
func RegisterCardAPI(mux *http.ServeMux, coreSvc *core.Service, st *store.Store, disp *callback.Dispatcher) {
	// 客户端：模板卡片交互（按钮 / 选项确认）
	mux.HandleFunc("POST /api/card/interact", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Userid    string         `json:"userid"`
			Msgid     string         `json:"msgid"`
			EventKey  string         `json:"event_key"`
			TaskID    string         `json:"task_id"`
			Selection map[string]any `json:"button_selection"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Msgid == "" || req.Userid == "" {
			http.Error(w, "msgid and userid required", http.StatusBadRequest)
			return
		}
		msg, err := st.GetMessageByMsgid(req.Msgid)
		if err != nil {
			http.Error(w, "message not found", http.StatusNotFound)
			return
		}
		if msg.SenderTyp != "bot" || msg.MsgType != "template_card" {
			http.Error(w, "not a bot template_card message", http.StatusBadRequest)
			return
		}
		bot, err := st.GetBot(msg.SenderID)
		if err != nil {
			http.Error(w, "bot missing", http.StatusInternalServerError)
			return
		}
		user, err := st.GetUserByUserid(req.Userid)
		if err != nil {
			http.Error(w, "unknown user", http.StatusBadRequest)
			return
		}
		if req.TaskID == "" {
			req.TaskID = msg.Msgid
		}
		var extra map[string]any
		if len(req.Selection) > 0 {
			extra = map[string]any{"button_selection": req.Selection}
		}
		disp.EnqueueCardEvent(msg, bot, user, req.EventKey, req.TaskID, extra)
		writeJSON(w, map[string]any{"errcode": 0, "errmsg": "ok"})
	})

	// 接入方：更新模板卡片（事件回调的 response_url 即指向本接口）
	mux.HandleFunc("POST /cgi-bin/aibot/update_template_card", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("response_code")
		if code == "" {
			writeErrcode(w, errcodeBadResponseCode, "response_code 缺失")
			return
		}
		task, err := st.ValidResponseTask(code)
		if err != nil {
			writeErrcode(w, errcodeBadResponseCode, "response_code "+err.Error())
			return
		}
		var req struct {
			TemplateCard map[string]any `json:"template_card"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if len(req.TemplateCard) == 0 {
			writeErrcode(w, errcodeBadContent, "invalid content, template_card empty")
			return
		}
		msg, err := st.MessageByID(task.MessageID)
		if err != nil {
			writeErrcode(w, 500, "internal error")
			return
		}
		if msg.MsgType != "template_card" {
			writeErrcode(w, 500, "not a template_card message")
			return
		}
		updated, err := st.UpdateMessageContent(msg.ID, req.TemplateCard)
		if err != nil {
			writeErrcode(w, 500, "internal error")
			return
		}
		// 不占用 response_code：1 小时内可多次更新卡片（对齐官方语义），客户端按 stream 覆盖刷新
		coreSvc.BroadcastStream(updated)
		writeErrcode(w, errcodeOK, "ok")
	})
}
