// Package botws 实现智能机器人长连接模式（M3）：接入方 SDK 以 wss 连入，
// 先发 aibot_subscribe（bot_id + secret）鉴权；平台以 aibot_msg_callback /
// aibot_event_callback 帧推送用户消息与事件，接入方以 aibot_respond_msg 帧回复。
// 与回调 URL 模式并存：机器人有活跃长连接时优先走 WS，否则回落到 HTTP 回调。
package botws

import (
	"errors"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// AuthFunc 校验订阅（aibotid + secret），返回机器人数字 ID；不合法返回 ok=false。
type AuthFunc func(aibotid, secret string) (botID int64, ok bool)

// FrameType 帧类型（对齐官方长连接帧名）。
const (
	FrameSubscribe     = "aibot_subscribe"
	FrameSubscribeResp = "aibot_subscribe_resp"
	FrameMsgCallback   = "aibot_msg_callback"
	FrameEventCallback = "aibot_event_callback"
	FrameRespond       = "aibot_respond_msg"
	FrameRespondResp   = "aibot_respond_msg_resp"
)

// Hub 管理全部已订阅的机器人长连接。
type Hub struct {
	mu      sync.RWMutex
	clients map[int64]*client
	auth    AuthFunc
	onFrame func(botID int64, frame map[string]any)
}

// NewHub 创建长连接 hub；auth 由上层提供（查库校验 secret）。
func NewHub(auth AuthFunc) *Hub {
	return &Hub{
		clients: map[int64]*client{},
		auth:    auth,
	}
}

// SetOnFrame 注册"收到接入方帧"回调（dispatcher 用它路由被动回复）。
func (h *Hub) SetOnFrame(f func(botID int64, frame map[string]any)) { h.onFrame = f }

// Has 返回机器人是否有活跃长连接。
func (h *Hub) Has(botID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[botID]
	return ok
}

// Push 向机器人推送一帧（回调帧），未连接返回错误。
func (h *Hub) Push(botID int64, frame map[string]any) error {
	h.mu.RLock()
	c := h.clients[botID]
	h.mu.RUnlock()
	if c == nil {
		return errors.New("机器人长连接未建立")
	}
	return c.writeJSON(frame)
}

// Count 当前在线连接数（自测/控制台展示用）。
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

var upgrader = websocket.Upgrader{
	// 本地开发工具：允许任意来源
	CheckOrigin: func(*http.Request) bool { return true },
}

// Register 挂载 GET /cgi-bin/aibot/ws（接入方 SDK 连入点）。
func (h *Hub) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /cgi-bin/aibot/ws", h.serveWS)
}

func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()

	// 首帧必须是 aibot_subscribe
	var sub struct {
		Type   string `json:"type"`
		BotID  string `json:"bot_id"`
		Secret string `json:"secret"`
	}
	if err := c.ReadJSON(&sub); err != nil || sub.Type != FrameSubscribe {
		_ = c.WriteJSON(map[string]any{"type": FrameSubscribeResp, "code": 1, "msg": "subscribe 首帧缺失或非法"})
		return
	}
	botID, ok := h.auth(sub.BotID, sub.Secret)
	if !ok {
		_ = c.WriteJSON(map[string]any{"type": FrameSubscribeResp, "code": 1, "msg": "bot_id 或 secret 无效"})
		return
	}

	cl := &client{botID: botID, ws: c, hub: h}
	h.mu.Lock()
	h.clients[botID] = cl
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if h.clients[botID] == cl {
			delete(h.clients, botID)
		}
		h.mu.Unlock()
	}()

	_ = cl.writeJSON(map[string]any{"type": FrameSubscribeResp, "code": 0, "msg": "ok"})

	for {
		var frame map[string]any
		if err := c.ReadJSON(&frame); err != nil {
			return
		}
		// 帧类型解析用带引号的形式，防止 map 内嵌类型混淆
		if typ, _ := frame["type"].(string); typ == FrameSubscribe {
			continue
		}
		if h.onFrame != nil {
			h.onFrame(botID, frame)
		}
	}
}

// client 是单条机器人长连接。
type client struct {
	botID int64
	ws    *websocket.Conn
	hub   *Hub
	mu    sync.Mutex // 写串行化
}

func (c *client) writeJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws.WriteJSON(v)
}

// Frame helper: 便捷构造回调帧。
func CallbackFrame(msgtype string, fields map[string]any) map[string]any {
	out := map[string]any{"type": FrameMsgCallback, "msgtype": msgtype}
	for k, v := range fields {
		out[k] = v
	}
	return out
}

// EventFrame 构造事件帧（如 enter_agent）。
func EventFrame(event string, fields map[string]any) map[string]any {
	out := map[string]any{"type": FrameEventCallback, "event": event}
	for k, v := range fields {
		out[k] = v
	}
	return out
}

// ReplyFrame 解析接入方的 aibot_respond_msg 帧（返回通用 map，交由 dispatcher 解读）。
func ReplyFrame(frame map[string]any) (msgid string, ok bool) {
	if typ, _ := frame["type"].(string); typ != FrameRespond {
		return "", false
	}
	msgid, _ = frame["msgid"].(string)
	return msgid, msgid != ""
}
