package api

import (
	"net/http"

	"im-/internal/core"
	"im-/internal/store"
)

// response_url 主动回复错误码：官方文档未公布该路径的错误码，
// 平台统一用 40001（response_code 无效，含不存在/已用/已过期），
// 以 errmsg 区分；已在 docs/错误码对照表.md 标注。
const errcodeBadResponseCode = 40001

// respondReq 是 POST /cgi-bin/aibot/response 请求体。
type respondReq struct {
	MsgType  string `json:"msgtype"`
	Markdown *struct {
		Content string `json:"content"`
	} `json:"markdown"`
	TemplateCard map[string]any `json:"template_card"`
}

// RegisterResponse 挂载 POST /cgi-bin/aibot/response?response_code=xxx。
// 语义对齐企微：一次性、1 小时有效；群聊主动回复自动引用触发它的用户消息。
func RegisterResponse(mux *http.ServeMux, coreSvc *core.Service, st *store.Store) {
	mux.HandleFunc("POST /cgi-bin/aibot/response", func(w http.ResponseWriter, r *http.Request) {
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
		var req respondReq
		if !decodeJSON(w, r, &req) {
			return
		}

		switch req.MsgType {
		case "markdown":
			if req.Markdown == nil || req.Markdown.Content == "" {
				writeErrcode(w, errcodeBadContent, "invalid content, markdown empty")
				return
			}
			if n := len(req.Markdown.Content); n > maxMarkdownBytes {
				writeErrcode(w, errcodeContentTooBig, "content size out of range")
				return
			}
		case "template_card":
			// M1b 支持
			writeErrcode(w, errcodeBadMsgType, "template_card 将在 M1b 支持")
			return
		default:
			writeErrcode(w, errcodeBadMsgType, "invalid msgtype")
			return
		}

		// 一次性语义：原子占用，失败说明已被使用或已过期
		ok, err := st.ConsumeResponseTask(code)
		if err != nil || !ok {
			writeErrcode(w, errcodeBadResponseCode, "response_code 已使用或已过期")
			return
		}

		msg, err := st.MessageByID(task.MessageID)
		if err != nil {
			writeErrcode(w, 500, "internal error")
			return
		}
		content := map[string]any{
			"content": req.Markdown.Content,
			"quote": map[string]any{
				"msgid":   msg.Msgid,
				"sender":  senderName(st, msg),
				"content": msg.Content["content"],
			},
		}
		if _, err := coreSvc.BotMessage(msg.ChatID, task.BotID, req.MsgType, content, nil); err != nil {
			writeErrcode(w, 500, "internal error")
			return
		}
		writeErrcode(w, errcodeOK, "ok")
	})
}

// senderName 取消息发送者展示名（引用展示用）。
func senderName(st *store.Store, m store.Message) string {
	if m.SenderTyp == "bot" {
		if b, err := st.GetBot(m.SenderID); err == nil {
			return b.Name
		}
		return "bot"
	}
	if u, err := st.GetUserByID(m.SenderID); err == nil {
		return u.Name
	}
	return "user"
}
