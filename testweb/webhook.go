package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// WebhookPayload 是推送给订阅方的消息体，模拟真实 IM 平台的回调格式。
type WebhookPayload struct {
	MsgID int64  `json:"msg_id"`
	User  string `json:"user"`
	Text  string `json:"text"`
	Kind  string `json:"kind"`
	TS    int64  `json:"ts"`
}

// WebhookManager 管理已注册的回调地址，消息到达时异步推送。
// 推送失败会重试（最多 3 次），模拟真实 IM 平台的重推行为，
// 订阅方需按 msg_id 自行做幂等去重。
type WebhookManager struct {
	mu    sync.RWMutex
	urls  map[string]struct{}
	client *http.Client
}

func NewWebhookManager() *WebhookManager {
	return &WebhookManager{
		urls:   make(map[string]struct{}),
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (wm *WebhookManager) Add(url string) {
	wm.mu.Lock()
	wm.urls[url] = struct{}{}
	wm.mu.Unlock()
}

func (wm *WebhookManager) Remove(url string) {
	wm.mu.Lock()
	delete(wm.urls, url)
	wm.mu.Unlock()
}

func (wm *WebhookManager) List() []string {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	out := make([]string, 0, len(wm.urls))
	for u := range wm.urls {
		out = append(out, u)
	}
	return out
}

// Notify 把一条新消息推送给所有订阅方（异步，不阻塞消息主链路）。
func (wm *WebhookManager) Notify(m Message) {
	urls := wm.List()
	if len(urls) == 0 {
		return
	}
	p := WebhookPayload{MsgID: m.ID, User: m.User, Text: m.Text, Kind: m.Kind, TS: m.TS}
	body, err := json.Marshal(p)
	if err != nil {
		log.Printf("marshal webhook payload: %v", err)
		return
	}
	for _, url := range urls {
		go wm.deliver(url, body)
	}
}

// deliver 投递并做有限重试，模拟 IM 平台「应答失败稍后重推」的语义。
func (wm *WebhookManager) deliver(url string, body []byte) {
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			cancel()
			log.Printf("webhook %s: build request: %v", url, err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := wm.client.Do(req)
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			_ = resp.Body.Close()
			cancel()
			return
		}
		if err == nil {
			_ = resp.Body.Close()
			log.Printf("webhook %s: attempt %d got status %d, will retry", url, attempt+1, resp.StatusCode)
		} else {
			log.Printf("webhook %s: attempt %d failed: %v", url, attempt+1, err)
		}
		cancel()
	}
}
