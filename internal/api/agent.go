package api

import (
	"net/http"
	"strings"

	"im-/internal/core"
	"im-/internal/store"
)

// 自建应用错误码（docs/错误码对照表.md 维护出处）。
const (
	errcodeInvalidCredential = 40001 // corpsecret 或 corpid 无效
	errcodeMissingToken      = 41001 // 缺少 access_token
	errcodeInvalidToken      = 40014 // access_token 无效
	errcodeTokenExpired      = 42001 // access_token 已过期
	errcodeMissingAgentID    = 41009 // 缺少 agentid
)

// RegisterAgentAPI 挂载自建应用接口：
//   - GET  /cgi-bin/gettoken
//   - POST /cgi-bin/message/send
func RegisterAgentAPI(mux *http.ServeMux, coreSvc *core.Service, st *store.Store) {
	mux.HandleFunc("GET /cgi-bin/gettoken", func(w http.ResponseWriter, r *http.Request) {
		corpid := r.URL.Query().Get("corpid")
		secret := r.URL.Query().Get("corpsecret")
		if corpid == "" || secret == "" {
			writeErrcode(w, errcodeInvalidCredential, "invalid corpid or corpsecret")
			return
		}
		corp, err := st.FirstCorp()
		if err != nil || corp.CorpID != corpid {
			writeErrcode(w, errcodeInvalidCredential, "invalid corpid")
			return
		}
		agent, err := st.GetAgentBySecret(secret)
		if err != nil {
			writeErrcode(w, errcodeInvalidCredential, "invalid corpsecret")
			return
		}
		tk, exp, err := st.IssueToken(agent.ID)
		if err != nil {
			writeErrcode(w, 500, "internal error")
			return
		}
		writeJSON(w, map[string]any{
			"errcode":      0,
			"errmsg":       "ok",
			"access_token": tk,
			"expires_in":   exp - unixNow(),
		})
	})

	mux.HandleFunc("POST /cgi-bin/message/send", func(w http.ResponseWriter, r *http.Request) {
		tk := r.URL.Query().Get("access_token")
		if tk == "" {
			writeErrcode(w, errcodeMissingToken, "access_token missing")
			return
		}
		agentID, err := st.ValidateToken(tk)
		if err != nil {
			// 过期与无效分别返回企微同款错误码，驱动接入方 SDK 刷新
			if strings.Contains(err.Error(), "expired") {
				writeErrcode(w, errcodeTokenExpired, "access_token expired")
			} else {
				writeErrcode(w, errcodeInvalidToken, "invalid access_token")
			}
			return
		}
		agent, err := st.GetAgent(agentID)
		if err != nil {
			writeErrcode(w, errcodeInvalidToken, "invalid access_token")
			return
		}

		var req struct {
			ToUser  string `json:"touser"`
			MsgType string `json:"msgtype"`
			AgentID int64  `json:"agentid"`
			Text    *struct {
				Content string `json:"content"`
			} `json:"text"`
			Markdown *struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.AgentID == 0 {
			writeErrcode(w, errcodeMissingAgentID, "agentid missing")
			return
		}
		if req.AgentID != agent.Agentid {
			writeErrcode(w, 40056, "invalid agentid") // 40056: 不合法的 agentid
			return
		}

		var content map[string]any
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
			content = map[string]any{"content": req.Text.Content}
		case "markdown":
			if req.Markdown == nil || req.Markdown.Content == "" {
				writeErrcode(w, errcodeBadContent, "invalid content, markdown empty")
				return
			}
			if n := len(req.Markdown.Content); n > maxMarkdownBytes {
				writeErrcode(w, errcodeContentTooBig, "content size out of range")
				return
			}
			content = map[string]any{"content": req.Markdown.Content}
		default:
			writeErrcode(w, errcodeBadMsgType, "invalid msgtype")
			return
		}

		if req.ToUser == "" {
			writeErrcode(w, 44004, "empty user list") // 44004: 内容为空（接收者列表为空）
			return
		}
		var invalid []string
		for _, userid := range strings.Split(req.ToUser, "|") {
			userid = strings.TrimSpace(userid)
			if userid == "" {
				continue
			}
			u, err := st.GetUserByUserid(userid)
			if err != nil {
				invalid = append(invalid, userid)
				continue
			}
			// 自建应用消息落在"用户↔应用"的单聊会话
			chat, err := st.CreateDirectChat(agent.ID, u.ID, agent.Name)
			if err != nil {
				writeErrcode(w, 500, "internal error")
				return
			}
			if _, err := coreSvc.AgentMessage(chat.ID, agent.ID, req.MsgType, content); err != nil {
				writeErrcode(w, 500, "internal error")
				return
			}
		}
		writeJSON(w, map[string]any{
			"errcode":     0,
			"errmsg":      "ok",
			"invaliduser": strings.Join(invalid, "|"),
		})
	})
}
