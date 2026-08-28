package botws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// 测试订阅鉴权、帧推送与接入方回复帧路由。
func TestHubSubscribePushRespond(t *testing.T) {
	var (
		received atomic.Int32
		gotFrame atomic.Value // map[string]any
	)
	hub := NewHub(func(aibotid, secret string) (int64, bool) {
		if aibotid == "wb_test" && secret == "s3cret" {
			return 42, true
		}
		return 0, false
	})
	hub.SetOnFrame(func(botID int64, frame map[string]any) {
		if botID != 42 {
			return
		}
		received.Add(1)
		gotFrame.Store(frame)
	})

	mux := http.NewServeMux()
	hub.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/cgi-bin/aibot/ws"

	t.Run("非法密钥拒绝订阅", func(t *testing.T) {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer c.Close()
		_ = c.WriteJSON(map[string]any{"type": "aibot_subscribe", "bot_id": "wb_test", "secret": "wrong"})
		var resp map[string]any
		if err := c.ReadJSON(&resp); err != nil {
			t.Fatalf("read resp: %v", err)
		}
		if code, _ := resp["code"].(float64); code == 0 {
			t.Fatalf("非法密钥不应订阅成功: %v", resp)
		}
	})

	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.WriteJSON(map[string]any{"type": "aibot_subscribe", "bot_id": "wb_test", "secret": "s3cret"}); err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := c.ReadJSON(&resp); err != nil {
		t.Fatal(err)
	}
	if code, _ := resp["code"].(float64); code != 0 {
		t.Fatalf("订阅失败: %v", resp)
	}
	if !hub.Has(42) {
		t.Fatal("订阅成功后应登记在线连接")
	}

	// 平台推送回调帧 → 接入方收到
	if err := hub.Push(42, CallbackFrame("text", map[string]any{
		"msgid": "M1", "aibotid": "wb_test", "chattype": "group",
		"text": map[string]any{"content": "hi"},
	})); err != nil {
		t.Fatalf("push: %v", err)
	}
	var frame map[string]any
	if err := c.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	if frame["type"] != "aibot_msg_callback" || frame["msgid"] != "M1" || frame["msgtype"] != "text" {
		t.Fatalf("回调帧异常: %v", frame)
	}

	// 接入方回复 aibot_respond_msg → 路由到 onFrame
	if err := c.WriteJSON(map[string]any{
		"type": "aibot_respond_msg", "msgid": "M1", "msgtype": "stream",
		"stream": map[string]any{"finish": true, "content": "pong"},
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for received.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if received.Load() != 1 {
		t.Fatalf("onFrame 未收到回复帧")
	}
	if f, _ := gotFrame.Load().(map[string]any); f["msgtype"] != "stream" {
		t.Fatalf("回复帧路由异常: %v", gotFrame.Load())
	}

	c.Close()
	// 断开后 Hub 应清理连接
	deadline = time.Now().Add(3 * time.Second)
	for hub.Has(42) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.Has(42) {
		t.Fatal("连接断开后应清理登记")
	}
	if err := hub.Push(42, map[string]any{"type": "aibot_msg_callback"}); err == nil {
		t.Fatal("未连接时应返回错误")
	}
}

// 确保帧 JSON 可正常序列化（dispatcher 端使用）。
func TestFrameMarshal(t *testing.T) {
	f := EventFrame("enter_agent", map[string]any{"msgid": "E1", "from": map[string]any{"userid": "u1"}})
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back["type"] != "aibot_event_callback" || back["event"] != "enter_agent" {
		t.Fatalf("事件帧异常: %s", b)
	}
	if _, ok := ReplyFrame(map[string]any{"type": "aibot_respond_msg", "msgid": "M1"}); !ok {
		t.Fatal("ReplyFrame 应识别 aibot_respond_msg")
	}
	if _, ok := ReplyFrame(map[string]any{"type": "aibot_msg_callback"}); ok {
		t.Fatal("ReplyFrame 不应识别回调帧")
	}
}
