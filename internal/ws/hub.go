// Package ws 是 Web 客户端的 WebSocket 网关：连接管理、事件广播。
package ws

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Conn 是一个已认证的客户端连接。
type Conn struct {
	Userid string
	ws     *websocket.Conn
	mu     sync.Mutex // 写串行化
}

func (c *Conn) writeJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws.WriteJSON(v)
}

// Hub 管理全部在线连接。M1a 单默认群：事件全量广播。
type Hub struct {
	mu    sync.RWMutex
	conns map[*Conn]struct{}
}

func NewHub() *Hub { return &Hub{conns: map[*Conn]struct{}{}} }

// Broadcast 向全部连接推送事件；失败连接静默丢弃（客户端有拉历史兜底）。
func (h *Hub) Broadcast(v any) {
	h.mu.RLock()
	conns := make([]*Conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.RUnlock()
	for _, c := range conns {
		_ = c.writeJSON(v)
	}
}

var upgrader = websocket.Upgrader{
	// 本地开发工具：允许任意来源
	CheckOrigin: func(*http.Request) bool { return true },
}

// Register 挂载 GET /ws?user=userid。
func (h *Hub) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		userid := r.URL.Query().Get("user")
		if userid == "" {
			http.Error(w, "missing user", http.StatusBadRequest)
			return
		}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn := &Conn{Userid: userid, ws: c}
		h.mu.Lock()
		h.conns[conn] = struct{}{}
		h.mu.Unlock()

		defer func() {
			h.mu.Lock()
			delete(h.conns, conn)
			h.mu.Unlock()
			c.Close()
		}()
		for {
			// 只读通道：客户端发消息走 HTTP /api/send；这里仅消费控制帧
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	})
}
