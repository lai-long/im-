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
	maxNewsArticles  = 8 // 图文消息最多 8 条
)

// webhookReq 是 webhook/send 请求体（M1a：text / markdown）。
type webhookReq struct {
	MsgType string `json:"msgtype"`
	Text    *struct {
		Content             string   `json:"content"`
		MentionedList       []string `json:"mentioned_list"`
		MentionedMobileList []string `json:"mentioned_mobile_list"`
	} `json:"text"`
	Markdown *struct {
		Content string `json:"content"`
	} `json:"markdown"`
	File *struct {
		MediaID string `json:"media_id"`
	} `json:"file"`
	Voice *struct {
		MediaID string `json:"media_id"`
	} `json:"voice"`
	Image *struct {
		Base64 string `json:"base64"`
		Md5    string `json:"md5"`
	} `json:"image"`
	News *struct {
		Articles []map[string]any `json:"articles"`
	} `json:"news"`
	TemplateCard map[string]any `json:"template_card"`
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
		case "file", "voice":
			body := req.File
			if req.MsgType == "voice" {
				body = req.Voice
			}
			if body == nil || body.MediaID == "" {
				writeErrcode(w, errcodeBadContent, "invalid content, media_id empty")
				return
			}
			if _, err := st.GetMedia(body.MediaID); err != nil {
				writeErrcode(w, 40007, "invalid media_id") // 40007: 不合法的 media_id
				return
			}
			if _, err := coreSvc.BotMessage(chat.ID, bot.ID, req.MsgType,
				map[string]any{"media_id": body.MediaID}, nil); err != nil {
				writeErrcode(w, 500, "internal error")
				return
			}
		case "markdown_v2":
			// markdown_v2 与 markdown 同载体，本地仅作类型透传（渲染一致）
			if req.Markdown == nil || req.Markdown.Content == "" {
				writeErrcode(w, errcodeBadContent, "invalid content, markdown empty")
				return
			}
			if n := len(req.Markdown.Content); n > maxMarkdownBytes {
				writeErrcode(w, errcodeContentTooBig, "content size out of range")
				return
			}
			if _, err := coreSvc.BotMessage(chat.ID, bot.ID, "markdown_v2",
				map[string]any{"content": req.Markdown.Content}, nil); err != nil {
				writeErrcode(w, 500, "internal error")
				return
			}
		case "image":
			if req.Image == nil || req.Image.Base64 == "" {
				writeErrcode(w, errcodeBadContent, "invalid content, image base64 empty")
				return
			}
			if _, err := coreSvc.BotMessage(chat.ID, bot.ID, "image",
				map[string]any{"base64": req.Image.Base64, "md5": req.Image.Md5}, nil); err != nil {
				writeErrcode(w, 500, "internal error")
				return
			}
		case "news":
			if req.News == nil || len(req.News.Articles) == 0 {
				writeErrcode(w, errcodeBadContent, "invalid content, news articles empty")
				return
			}
			if len(req.News.Articles) > maxNewsArticles {
				writeErrcode(w, errcodeContentTooBig, "news articles exceed limit")
				return
			}
			if _, err := coreSvc.BotMessage(chat.ID, bot.ID, "news",
				map[string]any{"articles": req.News.Articles}, nil); err != nil {
				writeErrcode(w, 500, "internal error")
				return
			}
		case "template_card":
			if req.TemplateCard == nil || len(req.TemplateCard) == 0 {
				writeErrcode(w, errcodeBadContent, "invalid content, template_card empty")
				return
			}
			if _, err := coreSvc.BotMessage(chat.ID, bot.ID, "template_card",
				req.TemplateCard, nil); err != nil {
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
