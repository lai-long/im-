// Package server 组装 HTTP 服务：企微兼容 API、客户端 API、WS 网关与嵌入页面。
package server

import (
	"embed"
	"io/fs"
	"net/http"

	"im-/internal/admin"
	"im-/internal/api"
	"im-/internal/botws"
	"im-/internal/callback"
	"im-/internal/config"
	"im-/internal/core"
	"im-/internal/store"
	"im-/internal/ws"
)

//go:embed web
var webFS embed.FS

// Server 是组装好的平台实例。
type Server struct {
	cfg        *config.Config
	st         *store.Store
	hub        *ws.Hub
	Core       *core.Service
	Dispatcher *callback.Dispatcher
	WSHub      *botws.Hub // 机器人长连接 hub（M3）
}

// New 初始化全部组件并返回。
func New(cfg *config.Config, st *store.Store) *Server {
	s := &Server{cfg: cfg, st: st, hub: ws.NewHub()}
	s.Core = core.New(st, func(ev core.Event) { s.hub.Broadcast(ev) })
	s.Dispatcher = callback.NewDispatcher(st, cfg.ExternalBaseURL(), s.Core)
	s.Core.OnBotMention = s.Dispatcher.EnqueueUserMessage
	s.Core.OnBotSingle = s.Dispatcher.EnqueueBotSingleMessage
	s.Core.OnBotEntry = func(chat store.Chat, bot store.Bot, user store.User) {
		s.Dispatcher.EnqueueBotEntry(chat, bot, user)
	}
	s.Core.OnAgentDirect = func(m store.Message, agentID int64) {
		agent, err := s.st.GetAgent(agentID)
		if err != nil {
			return
		}
		s.Dispatcher.EnqueueAgentMessage(m, agent)
	}

	// 机器人长连接（M3）：接入方 wss 订阅，有活跃连接时优先走长连接推送
	wsHub := botws.NewHub(func(aibotid, secret string) (int64, bool) {
		bot, err := st.GetBotByAibotid(aibotid)
		if err != nil || bot.Secret == "" || bot.Secret != secret {
			return 0, false
		}
		return bot.ID, true
	})
	s.WSHub = wsHub
	s.Dispatcher.SetWSHub(wsHub)
	return s
}

// Start 启动后台组件（回调消费循环）。
func (s *Server) Start() { s.Dispatcher.Start() }

// Handler 返回根路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 企微兼容层（M1a：webhook/send、aibot/response；后续：自建应用）
	api.RegisterWebhook(mux, s.Core, s.st)
	api.RegisterResponse(mux, s.Core, s.st)
	api.RegisterAgentAPI(mux, s.Core, s.st)              // 自建应用（M2）
	api.RegisterUploadMedia(mux, s.st)                   // 素材上传（M2）
	api.RegisterCardAPI(mux, s.Core, s.st, s.Dispatcher) // 模板卡片交互（M3）
	api.RegisterOAuthAPI(mux, s.Core, s.st)              // OAuth2 授权 + 应用群聊（M3）

	// 客户端内部 API 与 WS
	api.RegisterClientAPI(mux, s.Core, s.st)
	api.RegisterExportAPI(mux, s.st, s.hub) // 消息导出/回放（M3）
	s.hub.Register(mux)

	// 机器人长连接（M3）：wss 订阅端点
	s.WSHub.Register(mux)

	// 管理控制台 API
	admin.New(s.st, s.cfg, s.Dispatcher).Register(mux)

	// 嵌入式 Web 页面（群聊客户端 / 控制台）
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServer(http.FS(sub)))

	return mux
}
