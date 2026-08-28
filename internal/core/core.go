// Package core 是消息核心：落库、@解析、事件广播。
package core

import (
	"encoding/json"
	"regexp"
	"strings"

	"im-/internal/store"
)

// Service 消息核心服务。
type Service struct {
	st *store.Store
	// Broadcast 把事件推给 WS 网关等订阅方。
	Broadcast func(ev Event)
	// OnBotMention 在用户消息命中群内机器人时触发（回调分发器接入点）。
	OnBotMention func(msg store.Message, bot store.Bot)
}

// New 创建消息核心。broadcast 为空时不推送。
func New(st *store.Store, broadcast func(ev Event)) *Service {
	if broadcast == nil {
		broadcast = func(Event) {}
	}
	return &Service{st: st, Broadcast: broadcast}
}

// AgentMessage 自建应用（message/send）发一条消息到"用户↔应用"单聊会话。
func (s *Service) AgentMessage(chatID, agentID int64, msgType string, content map[string]any) (store.Message, error) {
	m, err := s.st.InsertMessage(chatID, agentID, "agent", msgType, content, nil)
	if err != nil {
		return m, err
	}
	s.broadcast(m)
	return m, nil
}

// Event 是推给客户端的实时事件。
type Event struct {
	Kind    string        `json:"kind"` // message
	Message store.Message `json:"message"`
}

var atTagRe = regexp.MustCompile(`<@([^>@]{1,64})>`)

// ParseMentions 解析 webhook/send 的 @ 语义，返回命中的 userid 列表。
// 来源：mentioned_list（userid / @all）、mentioned_mobile_list（M1a 忽略非 @all 项）、
// 内容中的 `<@userid>` 扩展语法。命中值限定为群成员内存在的 userid。
func (s *Service) ParseMentions(chatID int64, content string, mentionedList, mentionedMobileList []string) []string {
	var raw []string
	raw = append(raw, mentionedList...)
	raw = append(raw, mentionedMobileList...)
	for _, m := range atTagRe.FindAllStringSubmatch(content, -1) {
		raw = append(raw, m[1])
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		if v == "@all" {
			out = append(out, "@all")
			continue
		}
		if u, err := s.st.GetUserByUserid(v); err == nil {
			if _, err := s.st.FirstChatOfUser(u.ID); err == nil {
				out = append(out, u.Userid)
			}
		}
		// 未知 userid / 手机号：对齐企微行为——忽略，不报错
	}
	return out
}

// BotMessage 机器人（webhook/send / 回复路径）发一条消息到群。
func (s *Service) BotMessage(chatID, botID int64, msgType string, content map[string]any, mentioned []string) (store.Message, error) {
	m, err := s.st.InsertMessage(chatID, botID, "bot", msgType, content, mentioned)
	if err != nil {
		return m, err
	}
	s.broadcast(m)
	return m, nil
}

// UserMessage 用户在群里发一条消息（客户端 /api/send），返回消息与命中的群内机器人。
// 命中机器人（内容中含 @机器人名）的回调生成在 callback 分发器任务中处理。
func (s *Service) UserMessage(chatID, userID int64, text string) (store.Message, []store.Bot, error) {
	m, err := s.st.InsertMessage(chatID, userID, "user", "text",
		map[string]any{"content": text}, nil)
	if err != nil {
		return m, nil, err
	}
	s.broadcast(m)

	bots, err := s.st.ChatBots(chatID)
	if err != nil {
		return m, nil, err
	}
	var hit []store.Bot
	for _, b := range bots {
		if strings.Contains(text, "@"+b.Name) {
			hit = append(hit, b)
			if s.OnBotMention != nil {
				s.OnBotMention(m, b)
			}
		}
	}
	return m, hit, nil
}

// BroadcastStream 向客户端推送流式消息的全量刷新（同一 msgid 覆盖更新）。
func (s *Service) BroadcastStream(m store.Message) {
	s.Broadcast(Event{Kind: "stream", Message: m})
}

func (s *Service) broadcast(m store.Message) {
	raw, _ := json.Marshal(m.Content)
	_ = raw
	s.Broadcast(Event{Kind: "message", Message: m})
}
