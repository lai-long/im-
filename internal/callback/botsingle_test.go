package callback

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"im-/internal/core"
	"im-/internal/store"
)

// TestBotSingleChat 覆盖机器人单聊：chattype=single、无 chatid、被动回复落回单聊、进入会话事件欢迎语。
func TestBotSingleChat(t *testing.T) {
	st, _, bot := setup(t)
	csvc := core.New(st, nil)
	d := NewDispatcher(st, "http://127.0.0.1:7788", csvc)
	csvc.OnBotSingle = d.EnqueueBotSingleMessage
	csvc.OnBotEntry = func(ch store.Chat, b store.Bot, u store.User) {
		d.EnqueueBotEntry(ch, b, u)
	}
	d.Start()
	defer d.Stop()

	u, _ := st.GetUserByUserid("zhangsan")

	var got atomic.Value      // 最近一次回调明文
	var chatType atomic.Value // 最近一次 chattype
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		if c, ok := p["chattype"].(string); ok {
			chatType.Store(c)
		}
		got.Store(string(raw))
		reply := map[string]any{"msgtype": "stream",
			"stream": map[string]any{"id": "s", "finish": true, "content": "你好，我是机器人"}}
		if p["msgtype"] == "event" {
			reply["stream"].(map[string]any)["content"] = "欢迎来到单聊"
		}
		_, _ = w.Write([]byte(mustJSON(reply)))
	}))
	defer srv.Close()
	if err := st.UpdateBotCallback(bot.ID, srv.URL, tok, aesK, "plain"); err != nil {
		t.Fatal(err)
	}

	// 打开单聊（首次 → 进入会话事件）
	chat, created, err := st.OpenBotSingleChat(bot.ID, u.ID, bot.Name)
	if err != nil || !created {
		t.Fatalf("打开单聊失败: created=%v err=%v", created, err)
	}
	d.EnqueueBotEntry(chat, bot, u) // 首次进入触发"进入会话"事件
	if !waitForChatMsgPresent(st, chat.ID, "欢迎来到单聊") {
		t.Fatal("进入会话欢迎语未落单聊")
	}

	// 用户在单聊发消息
	if _, _, err := csvc.UserMessage(chat.ID, u.ID, "在吗"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got.Load() != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	payload, _ := got.Load().(string)
	if !strings.Contains(payload, `"chattype":"single"`) {
		t.Fatalf("单聊回调 chattype 应为 single: %s", payload)
	}
	if strings.Contains(payload, `"chatid"`) {
		t.Fatalf("单聊回调不应含 chatid: %s", payload)
	}
	if !waitForChatMsgPresent(st, chat.ID, "你好，我是机器人") {
		t.Fatal("单聊文本回复未落群")
	}
	_ = chatType
}

func waitForChatMsgPresent(st *store.Store, chatID int64, content string) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		msgs, _ := st.ListMessages(chatID, 50)
		for _, m := range msgs {
			if c, ok := m.Content["content"].(string); ok && strings.Contains(c, content) {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
