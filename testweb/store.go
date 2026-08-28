package main

import (
	"sync"
	"time"
)

// Message 是一条聊天消息或系统事件（加入/离开）。
type Message struct {
	ID   int64  `json:"id"`
	User string `json:"user"`
	Text string `json:"text"`
	Kind string `json:"kind"` // chat | join | leave
	TS   int64  `json:"ts"`   // 毫秒时间戳
}

const maxHistory = 500

// Store 是内存消息存储，按插入顺序保留最近 maxHistory 条。
type Store struct {
	mu       sync.RWMutex
	messages []Message
	nextID   int64
}

func NewStore() *Store {
	return &Store{nextID: 1}
}

// Append 追加一条消息并返回带 ID 和时间戳的完整消息。
func (s *Store) Append(user, text, kind string) Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := Message{
		ID:   s.nextID,
		User: user,
		Text: text,
		Kind: kind,
		TS:   time.Now().UnixMilli(),
	}
	s.nextID++
	s.messages = append(s.messages, m)
	if len(s.messages) > maxHistory {
		s.messages = s.messages[len(s.messages)-maxHistory:]
	}
	return m
}

// Recent 返回最近 limit 条消息（按时间升序），limit<=0 表示全部。
func (s *Store) Recent(limit int) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs := s.messages
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	out := make([]Message, len(msgs))
	copy(out, msgs)
	return out
}
