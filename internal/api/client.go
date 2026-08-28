package api

import (
	"net/http"

	"im-/internal/core"
	"im-/internal/store"
)

// RegisterClientAPI 挂载 /api/*（平台内部客户端接口，非企微兼容）。
func RegisterClientAPI(mux *http.ServeMux, coreSvc *core.Service, st *store.Store) {
	mux.HandleFunc("GET /api/messages", func(w http.ResponseWriter, r *http.Request) {
		chat, ok := firstChat(st)
		if v := r.URL.Query().Get("chat_id"); v != "" {
			if id, err := atoi64(v); err == nil {
				if c, err := st.GetChat(id); err == nil {
					chat, ok = c, true
				}
			}
		}
		if !ok {
			writeJSON(w, []any{})
			return
		}
		msgs, err := st.ListMessages(chat.ID, atoiDefault(r.URL.Query().Get("limit"), 100))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, msgs)
	})

	// 客户端素材下载（页面渲染 file/voice 消息）
	mux.HandleFunc("GET /api/media/{media_id}", func(w http.ResponseWriter, r *http.Request) {
		serveMedia(w, st, r.PathValue("media_id"))
	})

	// 会话列表：群聊 + 与自建应用/机器人的单聊（M2）
	mux.HandleFunc("GET /api/chats", func(w http.ResponseWriter, r *http.Request) {
		u, err := st.GetUserByUserid(r.URL.Query().Get("userid"))
		if err != nil {
			writeJSON(w, []any{})
			return
		}
		chats, err := st.ChatsOfUser(u.ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, chats)
	})

	// 机器人列表（客户端发起单聊用）。
	mux.HandleFunc("GET /api/bots", func(w http.ResponseWriter, r *http.Request) {
		bots, err := st.ListBots()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, bots)
	})

	// 发起/打开与机器人的单聊（chattype=single）；首次打开触发"进入会话"事件。
	mux.HandleFunc("POST /api/chats/single", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Userid string `json:"userid"`
			BotID  int64  `json:"bot_id"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		u, err := st.GetUserByUserid(req.Userid)
		if err != nil {
			http.Error(w, "unknown user", http.StatusBadRequest)
			return
		}
		bot, err := st.GetBot(req.BotID)
		if err != nil {
			http.Error(w, "unknown bot", http.StatusBadRequest)
			return
		}
		chat, created, err := st.OpenBotSingleChat(bot.ID, u.ID, bot.Name)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if created && coreSvc.OnBotEntry != nil {
			coreSvc.OnBotEntry(chat, bot, u)
		}
		writeJSON(w, chat)
	})

	mux.HandleFunc("POST /api/send", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Userid string `json:"userid"`
			Text   string `json:"text"`
			ChatID int64  `json:"chat_id"` // 可选：指定会话，缺省用用户首个群
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		u, err := st.GetUserByUserid(req.Userid)
		if err != nil {
			http.Error(w, "unknown user", http.StatusBadRequest)
			return
		}
		chat, err := st.FirstChatOfUser(u.ID)
		if req.ChatID != 0 {
			if c, cerr := st.GetChat(req.ChatID); cerr == nil {
				chat = c
			}
		}
		if err != nil && req.ChatID == 0 {
			http.Error(w, "user not in any chat", http.StatusBadRequest)
			return
		}
		if _, _, err := coreSvc.UserMessage(chat.ID, u.ID, req.Text); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]any{"errcode": 0})
	})

	mux.HandleFunc("GET /api/users", func(w http.ResponseWriter, r *http.Request) {
		users, err := st.ListUsers()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, users)
	})

	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Userid string `json:"userid"` // 选择预置用户
			Name   string `json:"name"`   // 或输入昵称自动注册
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Userid != "" {
			u, err := st.GetUserByUserid(req.Userid)
			if err != nil {
				http.Error(w, "unknown user", http.StatusBadRequest)
				return
			}
			writeJSON(w, u)
			return
		}
		if req.Name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		// 同名复用（多开标签模拟同一用户）
		if users, err := st.ListUsers(); err == nil {
			for _, u := range users {
				if u.Name == req.Name {
					writeJSON(w, u)
					return
				}
			}
		}
		corp, err := st.FirstCorp()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		u, err := st.CreateUser(corp.ID, req.Name)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, u)
	})
}

// firstChat 返回首个群（M1a 单默认群模型）。
func firstChat(st *store.Store) (store.Chat, bool) {
	chats, err := st.ListChats()
	if err != nil || len(chats) == 0 {
		return store.Chat{}, false
	}
	return chats[0], true
}
