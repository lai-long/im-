package api

import (
	"net/http"
	"net/url"
	"strings"

	"im-/internal/core"
	"im-/internal/store"
)

// OAuth2 授权码有效期（企微：5 分钟）。
const oauthCodeTTL = 300

// RegisterOAuthAPI 挂载 OAuth2 网页授权与应用群聊接口（M3）：
//   - GET  /cgi-bin/oauth2/authorize            授权页入口（本地：自动授权，302 回跳 code）
//   - GET  /cgi-bin/user/getuserinfo            用 code 换用户信息（userid）
//   - POST /cgi-bin/appchat/send                应用群聊发消息
func RegisterOAuthAPI(mux *http.ServeMux, coreSvc *core.Service, st *store.Store) {
	// 网页授权：appid=corpid&redirect_uri=...&state=...&userid=<本地指定>（缺省 zhangsan）
	mux.HandleFunc("GET /cgi-bin/oauth2/authorize", func(w http.ResponseWriter, r *http.Request) {
		corp, err := st.FirstCorp()
		if err != nil || r.URL.Query().Get("appid") != corp.CorpID {
			http.Error(w, "invalid appid", http.StatusBadRequest)
			return
		}
		redirectURI := r.URL.Query().Get("redirect_uri")
		if redirectURI == "" {
			http.Error(w, "redirect_uri required", http.StatusBadRequest)
			return
		}
		userid := r.URL.Query().Get("userid")
		if userid == "" {
			userid = "zhangsan" // 本地工具缺省授权用户
		}
		u, err := st.GetUserByUserid(userid)
		if err != nil {
			http.Error(w, "unknown userid", http.StatusBadRequest)
			return
		}
		code, err := st.CreateOAuthCode(u.Userid, oauthCodeTTL)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// 302 回跳：redirect_uri?code=xxx&state=yyy（保留 redirect_uri 自带查询）
		loc := redirectURI
		if strings.Contains(redirectURI, "?") {
			loc += "&"
		} else {
			loc += "?"
		}
		q := url.Values{"code": {code}}
		if state := r.URL.Query().Get("state"); state != "" {
			q.Set("state", state)
		}
		loc += q.Encode()
		w.Header().Set("Location", loc)
		w.WriteHeader(http.StatusFound)
	})

	// 用 code 换 userid（对齐企微 getuserinfo）
	mux.HandleFunc("GET /cgi-bin/user/getuserinfo", func(w http.ResponseWriter, r *http.Request) {
		if requireToken(w, r, st) == 0 {
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			writeErrcode(w, 40029, "invalid code")
			return
		}
		userid, err := st.ConsumeOAuthCode(code)
		switch {
		case err == nil:
		case err.Error() == "code already used":
			writeErrcode(w, 40163, "code already used") // 40163: code 已被使用
			return
		default:
			writeErrcode(w, 40029, err.Error()) // 40029: 不合法 code（不存在/过期）
			return
		}
		u, err := st.GetUserByUserid(userid)
		if err != nil {
			writeErrcode(w, 40029, "userid not found")
			return
		}
		writeJSON(w, map[string]any{"errcode": 0, "errmsg": "ok", "userid": u.Userid, "name": u.Name})
	})

	// 应用群聊：POST /cgi-bin/appchat/send?access_token=xxx
	mux.HandleFunc("POST /cgi-bin/appchat/send", func(w http.ResponseWriter, r *http.Request) {
		agentID := requireToken(w, r, st)
		if agentID == 0 {
			return
		}
		agent, err := st.GetAgent(agentID)
		if err != nil {
			writeErrcode(w, errcodeInvalidToken, "invalid access_token")
			return
		}
		var req struct {
			ChatID  string `json:"chatid"`
			MsgType string `json:"msgtype"`
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
		if req.ChatID == "" {
			req.ChatID = store.NewChatID() // 缺省自动建群
		}
		chat, _, err := st.GetOrCreateGroupChat(req.ChatID, "应用群聊 "+req.ChatID)
		if err != nil {
			writeErrcode(w, 500, "internal error")
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
		if _, err := coreSvc.AgentMessage(chat.ID, agent.ID, req.MsgType, content); err != nil {
			writeErrcode(w, 500, "internal error")
			return
		}
		writeErrcode(w, errcodeOK, "ok")
	})
}
