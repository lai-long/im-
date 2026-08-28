// testweb 是一个简易 IM 系统（HTTP + WebSocket），用于平台联调测试。
// 提供群聊页面、REST 消息接口和实时推送，数据全部保存在内存中。
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"strconv"
)

//go:embed web/dist
var distFS embed.FS

type Server struct {
	hub      *Hub
	store    *Store
	webhooks *WebhookManager
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// GET /api/messages?limit=N 获取历史消息（升序）。
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, http.StatusOK, s.store.Recent(limit))
}

// POST /api/messages {"user":"u","text":"hello"} 通过 HTTP 发消息。
// 该接口模拟 IM 主动发送/回调入口，便于无前端时做接口级测试。
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		User string `json:"user"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.User == "" || body.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "need json {user, text}"})
		return
	}
	writeJSON(w, http.StatusOK, s.hub.PostMessage(body.User, body.Text))
}

// GET /api/users 在线用户列表。
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.hub.OnlineUsers())
}

// webhook 回调订阅管理，模拟 IM 平台的「应用回调地址」配置。
// POST /api/webhooks {"url":"http://..."} 注册；GET 列表；DELETE ?url= 注销。
func (s *Server) handleWebhooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.webhooks.List())
	case http.MethodPost:
		var body struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "need json {url}"})
			return
		}
		s.webhooks.Add(body.URL)
		writeJSON(w, http.StatusOK, s.webhooks.List())
	case http.MethodDelete:
		s.webhooks.Remove(r.URL.Query().Get("url"))
		writeJSON(w, http.StatusOK, s.webhooks.List())
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// GET /ws?user=xxx 建立实时连接。
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user"})
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	s.hub.ServeWS(conn, user)
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	sub, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		log.Fatalf("load embedded assets: %v", err)
	}

	store := NewStore()
	webhooks := NewWebhookManager()
	s := &Server{hub: NewHub(store, webhooks), store: store, webhooks: webhooks}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/messages", s.handleMessages)
	mux.HandleFunc("/api/send", s.handleSend)
	mux.HandleFunc("/api/users", s.handleUsers)
	mux.HandleFunc("/api/webhooks", s.handleWebhooks)
	mux.HandleFunc("/ws", s.handleWS)
	mux.Handle("/", http.FileServer(http.FS(sub)))

	log.Printf("testweb IM listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
