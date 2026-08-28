package admin

import (
	"errors"
	"net/http"
	"strconv"
)

// ErrNoCallbackURL 回调 URL 未配置。
var errNoCallbackURL = errors.New("callback URL 未配置")

// Register 挂载 GET/POST /admin/* 控制台 API。
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/bots", s.bots)
	mux.HandleFunc("POST /admin/bots", s.createBot)
	mux.HandleFunc("POST /admin/bots/callback", s.saveCallback)
	mux.HandleFunc("POST /admin/bots/verify", s.verify)
	mux.HandleFunc("GET /admin/chats", s.chats)
	mux.HandleFunc("POST /admin/chats/join", s.joinChat)
	mux.HandleFunc("GET /admin/messages", s.messages)
	mux.HandleFunc("GET /admin/tasks", s.tasks)
	mux.HandleFunc("POST /admin/tasks/replay", s.replayTask)
}

func atoi64(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }
