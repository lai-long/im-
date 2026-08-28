// Package admin 是管理控制台：机器人/群管理、回调配置与在线验证、消息流水与回调重放。
package admin

import (
	"encoding/json"
	"net/http"

	"im-/internal/callback"
	"im-/internal/config"
	"im-/internal/store"
)

// Service 控制台服务。
type Service struct {
	st   *store.Store
	cfg  *config.Config
	disp *callback.Dispatcher
}

// New 创建控制台服务。
func New(st *store.Store, cfg *config.Config, disp *callback.Dispatcher) *Service {
	return &Service{st: st, cfg: cfg, disp: disp}
}

func (s *Service) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Service) err(w http.ResponseWriter, msg string, code int) {
	w.WriteHeader(code)
	s.writeJSON(w, map[string]any{"error": msg})
}

// botView 是控制台展示的机器人视图（含各群 webhook key）。
type botView struct {
	store.Bot
	Keys []store.ChatBotKey `json:"keys"`
}

func (s *Service) bots(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListBots()
	if err != nil {
		s.err(w, err.Error(), 500)
		return
	}
	out := make([]botView, 0, len(list))
	for _, b := range list {
		keys, _ := s.st.ChatBotKeys(b.ID)
		out = append(out, botView{Bot: b, Keys: keys})
	}
	s.writeJSON(w, out)
}

func (s *Service) createBot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		ChatID int64  `json:"chat_id"` // 可选：创建后立即拉入该群
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		s.err(w, "name required", http.StatusBadRequest)
		return
	}
	corp, err := s.st.FirstCorp()
	if err != nil {
		s.err(w, err.Error(), 500)
		return
	}
	b, err := s.st.CreateBot(corp.ID, req.Name)
	if err != nil {
		s.err(w, err.Error(), 500)
		return
	}
	if req.ChatID != 0 {
		if _, err := s.st.AddBotToChat(req.ChatID, b.ID); err != nil {
			s.err(w, err.Error(), 500)
			return
		}
	}
	keys, _ := s.st.ChatBotKeys(b.ID)
	s.writeJSON(w, botView{Bot: b, Keys: keys})
}

// saveCallback 保存回调配置并立即做一次 URL 验证握手（对齐企微"保存回调"行为）。
func (s *Service) saveCallback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BotID int64  `json:"bot_id"`
		URL   string `json:"url"`
		Mode  string `json:"mode"` // encrypted(默认) | plain
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BotID == 0 || req.URL == "" {
		s.err(w, "bot_id and url required", http.StatusBadRequest)
		return
	}
	if req.Mode == "" {
		req.Mode = "encrypted"
	}
	if req.Mode != "encrypted" && req.Mode != "plain" {
		s.err(w, "mode must be encrypted or plain", http.StatusBadRequest)
		return
	}
	bot, err := s.st.GetBot(req.BotID)
	if err != nil {
		s.err(w, "bot not found", http.StatusNotFound)
		return
	}
	if err := s.st.UpdateBotCallback(bot.ID, req.URL, bot.CallbackToken, bot.CallbackAESKey, req.Mode); err != nil {
		s.err(w, err.Error(), 500)
		return
	}
	out := map[string]any{"bot_id": bot.ID, "url": req.URL, "mode": req.Mode}
	if err := s.verifyBot(bot.ID); err != nil {
		out["verified"] = false
		out["error"] = err.Error()
	} else {
		out["verified"] = true
	}
	s.writeJSON(w, out)
}

func (s *Service) verify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BotID int64 `json:"bot_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BotID == 0 {
		s.err(w, "bot_id required", http.StatusBadRequest)
		return
	}
	if err := s.verifyBot(req.BotID); err != nil {
		s.writeJSON(w, map[string]any{"bot_id": req.BotID, "verified": false, "error": err.Error()})
		return
	}
	s.writeJSON(w, map[string]any{"bot_id": req.BotID, "verified": true})
}

func (s *Service) verifyBot(botID int64) error {
	bot, err := s.st.GetBot(botID)
	if err != nil {
		return err
	}
	if bot.CallbackURL == "" {
		return errNoCallbackURL
	}
	if err := s.disp.VerifyCallback(bot.CallbackURL, bot.CallbackToken, bot.CallbackAESKey); err != nil {
		return err
	}
	return s.st.MarkCallbackVerified(botID)
}

// chats 列出群。
func (s *Service) chats(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListChats()
	if err != nil {
		s.err(w, err.Error(), 500)
		return
	}
	s.writeJSON(w, list)
}

// joinChat 把机器人拉进群（生成该群 webhook key）。
func (s *Service) joinChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChatID int64 `json:"chat_id"`
		BotID  int64 `json:"bot_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChatID == 0 || req.BotID == 0 {
		s.err(w, "chat_id and bot_id required", http.StatusBadRequest)
		return
	}
	key, err := s.st.AddBotToChat(req.ChatID, req.BotID)
	if err != nil {
		s.err(w, err.Error(), 500)
		return
	}
	s.writeJSON(w, map[string]any{"chat_id": req.ChatID, "bot_id": req.BotID, "webhook_key": key})
}

// messages 消息流水（按群）。
func (s *Service) messages(w http.ResponseWriter, r *http.Request) {
	chatID := int64(0)
	if chats, err := s.st.ListChats(); err == nil && len(chats) > 0 {
		chatID = chats[0].ID
	}
	if v := r.URL.Query().Get("chat_id"); v != "" {
		if n, err := atoi64(v); err == nil {
			chatID = n
		}
	}
	msgs, err := s.st.ListMessages(chatID, 200)
	if err != nil {
		s.err(w, err.Error(), 500)
		return
	}
	s.writeJSON(w, msgs)
}

// tasks 回调任务列表（可按状态过滤）。
func (s *Service) tasks(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListCallbackTasks(r.URL.Query().Get("status"), 200)
	if err != nil {
		s.err(w, err.Error(), 500)
		return
	}
	s.writeJSON(w, list)
}

// replayTask 手动重推任意历史回调任务（联调排障用）。
func (s *Service) replayTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 {
		s.err(w, "id required", http.StatusBadRequest)
		return
	}
	if err := s.st.ResetCallbackTask(req.ID); err != nil {
		s.err(w, err.Error(), 500)
		return
	}
	s.writeJSON(w, map[string]any{"id": req.ID, "status": "pending"})
}
