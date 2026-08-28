package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	maxMsgSize = 4 << 10
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WSEvent 是 WebSocket 下行事件。
type WSEvent struct {
	Type    string   `json:"type"` // message | users
	Message *Message `json:"message,omitempty"`
	Users   []string `json:"users,omitempty"`
}

// Client 是一个 WebSocket 连接。
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	user string
	send chan []byte
}

// Hub 管理所有在线客户端并广播消息。
type Hub struct {
	mu       sync.RWMutex
	clients  map[*Client]struct{}
	store    *Store
	webhooks *WebhookManager
}

func NewHub(store *Store, webhooks *WebhookManager) *Hub {
	return &Hub{clients: make(map[*Client]struct{}), store: store, webhooks: webhooks}
}

// publish 落库、广播并触发 webhook 回调，是所有消息的统一出口。
func (h *Hub) publish(user, text, kind string) Message {
	m := h.store.Append(user, text, kind)
	h.broadcast(WSEvent{Type: "message", Message: &m})
	h.webhooks.Notify(m)
	return m
}

// OnlineUsers 返回当前在线用户名列表。
func (h *Hub) OnlineUsers() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	users := make([]string, 0, len(h.clients))
	for c := range h.clients {
		users = append(users, c.user)
	}
	return users
}

// PostMessage 落库并广播一条聊天消息，返回完整消息。
func (h *Hub) PostMessage(user, text string) Message {
	return h.publish(user, text, "chat")
}

func (h *Hub) broadcast(ev WSEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		log.Printf("marshal ws event: %v", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default: // 客户端消费太慢，断开以免阻塞其他人
			go h.remove(c)
		}
	}
}

func (h *Hub) add(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	h.publish(c.user, c.user+" 加入了聊天", "join")
	h.broadcast(WSEvent{Type: "users", Users: h.OnlineUsers()})
}

func (h *Hub) remove(c *Client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; !ok {
		h.mu.Unlock()
		return
	}
	delete(h.clients, c)
	close(c.send)
	h.mu.Unlock()
	_ = c.conn.Close()
	h.publish(c.user, c.user+" 离开了聊天", "leave")
	h.broadcast(WSEvent{Type: "users", Users: h.OnlineUsers()})
}

// incoming 是 WS 上行消息。
type incoming struct {
	Type string `json:"type"` // message
	Text string `json:"text"`
}

// ServeWS 处理一个 WebSocket 连接的完整生命周期。
func (h *Hub) ServeWS(conn *websocket.Conn, user string) {
	c := &Client{hub: h, conn: conn, user: user, send: make(chan []byte, 64)}
	h.add(c)
	defer h.remove(c)

	go c.writePump()

	conn.SetReadLimit(maxMsgSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var in incoming
		if err := json.Unmarshal(data, &in); err != nil {
			continue
		}
		if in.Type == "message" && in.Text != "" {
			h.PostMessage(c.user, in.Text)
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.hub.remove(c)
	}()
	for {
		select {
		case data, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
