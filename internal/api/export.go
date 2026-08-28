package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"im-/internal/core"
	"im-/internal/store"
	"im-/internal/ws"
)

// RegisterExportAPI 挂载消息导出与会话回放接口（M3）：
//   - GET  /api/export?chat_id=N[&format=csv]   导出会话消息（json 默认 / csv）
//   - POST /api/replay                           把会话历史按序经 WS 重推给指定用户（演示/排障）
func RegisterExportAPI(mux *http.ServeMux, st *store.Store, hub *ws.Hub) {
	mux.HandleFunc("GET /api/export", func(w http.ResponseWriter, r *http.Request) {
		chatID, err := strconv.ParseInt(r.URL.Query().Get("chat_id"), 10, 64)
		if err != nil {
			http.Error(w, "chat_id required", http.StatusBadRequest)
			return
		}
		msgs, err := st.ListMessages(chatID, 1000)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if r.URL.Query().Get("format") == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=chat_%d.csv", chatID))
			cw := csv.NewWriter(w)
			_ = cw.Write([]string{"ts", "sender", "sender_type", "msgtype", "content"})
			for _, m := range msgs {
				cj, _ := json.Marshal(m.Content)
				_ = cw.Write([]string{
					strconv.FormatInt(m.CreatedAt, 10), m.Sender, m.SenderTyp, m.MsgType, string(cj),
				})
			}
			cw.Flush()
			return
		}
		writeJSON(w, msgs)
	})

	mux.HandleFunc("POST /api/replay", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Userid string `json:"userid"`
			ChatID int64  `json:"chat_id"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if _, err := st.GetUserByUserid(req.Userid); err != nil {
			http.Error(w, "unknown user", http.StatusBadRequest)
			return
		}
		if _, err := st.GetChat(req.ChatID); err != nil {
			http.Error(w, "unknown chat", http.StatusBadRequest)
			return
		}
		msgs, err := st.ListMessages(req.ChatID, 1000)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// 按序重推，客户端仅在当前会话渲染，不重新落库
		for _, m := range msgs {
			hub.SendToUser(req.Userid, core.Event{Kind: "replay", Message: m})
		}
		writeJSON(w, map[string]any{"errcode": 0, "errmsg": "ok", "count": len(msgs)})
	})
}
