package api

import (
	"net/http"

	"im-/internal/core"
	"im-/internal/store"
)

// 企微群机器人 webhook/send 错误码（docs/错误码对照表.md 维护出处）。
const (
	errcodeOK            = 0
	errcodeBadKey        = 93000 // 无效 webhook key
	errcodeBotKicked     = 93001 // 机器人已被移出群（本地不触发，保留）
	errcodeBadMsgType    = 40058 // msgtype 不支持
	errcodeBadContent    = 40008 // 缺少 content
	errcodeContentTooBig = 45002 // content 超长
)

// 企微群机器人长度限制（字节）。
const (
	maxTextBytes     = 2048
	maxMarkdownBytes = 4096
)

// webhookReq 是 webhook/send 请求体（M1a：text / markdown）。
type webhookReq struct {
	MsgType  string `json:"msgtype"`
	Text     *struct {
		Content             string   `json:"content"`
		MentionedList       []string `json:"mentioned_list"`
		MentionedMobileList []string `json:"mentioned_mobile_list"`
	} `json:"text"`
	Markdown *struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

// RegisterWebhook 挂载 POST /cgi-bin/webhook/send?key=xxx。
func RegisterWebhook(mux *http.ServeMux, coreSvc *core.Service, st *store.Store) {
	mux.HandleFunc("POST /cgi-bin/webhook/send", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			writeErrcode(w, errcodeBadKey, "invalid webhook key")
			return
		}
		chat, bot, err := st.GetChatByWebhookKey(key)
		if err != nil {
			writeErrcode(w, errcodeBadKey, "invalid webhook key")
			return
		}

		var req webhookReq
		if !decodeJSON(w, r, &req) {
			return
		}

		switch req.MsgType {
		case "text":
			if req.Text == nil || req.Text.Content == "" {
				writeErrcode(w, errcodeBadContent, "invalid content, text empty")
				return
			}
			if n := len(req.Text.Content); n > maxTextBytes {
				writeErrcode(w, errcodeContentTooBig, "content size out of range")
				return
			}
			mentioned := coreSvc.ParseMentions(chat.ID, req.Text.Content, req.Text.MentionedList, req.Text.MentionedMobileList)
			if _, err := coreSvc.BotMessage(chat.ID, bot.ID, "text",
				map[string]any{"content": req.Text.Content}, mentioned); err != nil {
				writeErrcode(w, 500, "internal error")
				return
			}
		case "markdown":
			if req.Markdown == nil || req.Markdown.Content == "" {
				writeErrcode(w, errcodeBadContent, "invalid content, markdown empty")
				return
			}
			if n := len(req.Markdown.Content); n > maxMarkdownBytes {
				writeErrcode(w, errcodeContentTooBig, "content size out of range")
				return
			}
			if _, err := coreSvc.BotMessage(chat.ID, bot.ID, "markdown",
				map[string]any{"content": req.Markdown.Content}, nil); err != nil {
				writeErrcode(w, 500, "internal error")
				return
			}
		default:
			writeErrcode(w, errcodeBadMsgType, "invalid msgtype")
			return
		}
		writeErrcode(w, errcodeOK, "ok")
	})
}
