package callback

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	official "github.com/sbzhu/weworkapi_golang/wxbizmsgcrypt"

	"im-/internal/core"
	"im-/internal/store"
)

const (
	tok  = "testToken123"
	aesK = "jWmYm7qr9nMoAVwEjSiPJBmA1CQvL47GfIxCZWvAfdP"
)

// mockBot 模拟接入方：官方库验证 echostr、解密推送、返回被动回复。
type mockBot struct {
	srv       *httptest.Server
	crypt     *official.WXBizMsgCrypt
	pushes    atomic.Int32
	failFirst int32 // 前 N 次返回 500
	reply     func() map[string]any
}

func newMockBot(t *testing.T, failFirst int32, reply func() map[string]any) *mockBot {
	m := &mockBot{crypt: official.NewWXBizMsgCrypt(tok, aesK, "", official.XmlType), reply: reply, failFirst: failFirst}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// URL 验证：官方 VerifyURL 解密后回显明文
			plain, cerr := m.crypt.VerifyURL(r.URL.Query().Get("msg_signature"),
				r.URL.Query().Get("timestamp"), r.URL.Query().Get("nonce"),
				r.URL.Query().Get("echostr"))
			if cerr != nil {
				http.Error(w, "bad", http.StatusBadRequest)
				return
			}
			_, _ = w.Write(plain)
		case http.MethodPost:
			pushN := m.pushes.Add(1)
			if m.failFirst > 0 && pushN <= m.failFirst {
				http.Error(w, "down", http.StatusInternalServerError)
				return
			}
			// 用官方库解密我方 JSON 信封：先取出 encrypt 再包成 XML 交给官方
			var env struct {
				Encrypt string `json:"encrypt"`
			}
			_ = json.NewDecoder(r.Body).Decode(&env)
			msg, cerr := m.crypt.DecryptMsg(r.URL.Query().Get("msg_signature"),
				r.URL.Query().Get("timestamp"), r.URL.Query().Get("nonce"),
				[]byte("<xml><Encrypt><![CDATA["+env.Encrypt+"]]></Encrypt></xml>"))
			if cerr != nil {
				http.Error(w, "decrypt fail", http.StatusBadRequest)
				return
			}
			if !strings.Contains(string(msg), "@示例机器人 hello") {
				http.Error(w, "unexpected payload", http.StatusBadRequest)
				return
			}
			// 官方库加密被动回复，取 Encrypt 字段包 JSON 信封返回
			raw, cerr := m.crypt.EncryptMsg(mustJSON(m.reply()), r.URL.Query().Get("timestamp"), r.URL.Query().Get("nonce"))
			if cerr != nil {
				http.Error(w, "encrypt fail", http.StatusBadRequest)
				return
			}
			var out struct {
				Encrypt string `xml:"Encrypt"`
			}
			_ = xml.Unmarshal(raw, &out)
			_ = json.NewEncoder(w).Encode(map[string]string{"encrypt": out.Encrypt})
		}
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func setup(t *testing.T) (*store.Store, store.Chat, store.Bot) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	chat, bot, err := st.GetChatByWebhookKey(store.SeedWebhookKey)
	if err != nil {
		t.Fatal(err)
	}
	// 回调三元组（密文模式），URL 由各用例指向 mock
	bot.CallbackToken = tok
	bot.CallbackAESKey = aesK
	bot.CallbackMode = "encrypted"
	return st, chat, bot
}

func waitTaskStatus(t *testing.T, st *store.Store, status string) store.CallbackTask {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tasks, _ := st.ListCallbackTasks(status, 10)
		for _, ts := range tasks {
			if ts.Status == status {
				return ts
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("等待任务状态 %s 超时", status)
	return store.CallbackTask{}
}

func TestDispatcherHappyPath(t *testing.T) {
	st, chat, bot := setup(t)
	mock := newMockBot(t, 0, func() map[string]any {
		return map[string]any{"msgtype": "stream",
			"stream": map[string]any{"id": "s1", "finish": true, "content": "**pong**"}}
	})
	bot.CallbackURL = mock.srv.URL
	_ = st.UpdateBotCallback(bot.ID, mock.srv.URL, tok, aesK, "encrypted")

	csvc := core.New(st, nil)
	d := NewDispatcher(st, "http://127.0.0.1:7788", csvc)
	d.Backoff = []time.Duration{10 * time.Millisecond}
	csvc.OnBotMention = d.EnqueueUserMessage
	d.Start()
	defer d.Stop()

	// 用户 @机器人 发消息（先加入群）
	u, err := st.GetUserByUserid("zhangsan")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := csvc.UserMessage(chat.ID, u.ID, "@示例机器人 hello"); err != nil {
		t.Fatal(err)
	}

	task := waitTaskStatus(t, st, "done")
	if task.Attempt != 1 {
		t.Fatalf("期望一次推送成功, attempt=%d", task.Attempt)
	}
	// 被动回复落群
	msgs, _ := st.ListMessages(chat.ID, 50)
	var found bool
	for _, m := range msgs {
		if m.SenderTyp == "bot" && m.MsgType == "stream" {
			if m.Content["content"] == "**pong**" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("被动回复未落群: %+v", msgs)
	}
	// response_code 已签发且可校验
	if _, err := st.ValidResponseTask(task.ResponseCode); err != nil {
		t.Fatalf("response_code 应有效: %v", err)
	}
}

func TestDispatcherRetryThenSuccess(t *testing.T) {
	st, chat, bot := setup(t)
	mock := newMockBot(t, 2, func() map[string]any {
		return map[string]any{"msgtype": "stream",
			"stream": map[string]any{"id": "s1", "finish": true, "content": "ok"}}
	})
	_ = st.UpdateBotCallback(bot.ID, mock.srv.URL, tok, aesK, "encrypted")

	csvc := core.New(st, nil)
	d := NewDispatcher(st, "http://127.0.0.1:7788", csvc)
	d.Backoff = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond}
	csvc.OnBotMention = d.EnqueueUserMessage
	d.Start()
	defer d.Stop()

	u, _ := st.GetUserByUserid("lisi")
	if _, _, err := csvc.UserMessage(chat.ID, u.ID, "@示例机器人 hello"); err != nil {
		t.Fatal(err)
	}

	task := waitTaskStatus(t, st, "done")
	if task.Attempt != 3 {
		t.Fatalf("期望 3 次尝试后成功, attempt=%d", task.Attempt)
	}
	if mock.pushes.Load() != 3 {
		t.Fatalf("mock 应收到 3 次推送, got %d", mock.pushes.Load())
	}
}

func TestDispatcherDead(t *testing.T) {
	st, chat, bot := setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "always down", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_ = st.UpdateBotCallback(bot.ID, srv.URL, tok, aesK, "encrypted")

	csvc := core.New(st, nil)
	d := NewDispatcher(st, "http://127.0.0.1:7788", csvc)
	d.Backoff = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond}
	csvc.OnBotMention = d.EnqueueUserMessage
	d.Start()
	defer d.Stop()

	u, _ := st.GetUserByUserid("zhangsan")
	if _, _, err := csvc.UserMessage(chat.ID, u.ID, "@示例机器人 hello"); err != nil {
		t.Fatal(err)
	}

	task := waitTaskStatus(t, st, "dead")
	if task.Attempt != 4 { // 1 次初始 + 3 次重试
		t.Fatalf("期望 4 次尝试后死信, attempt=%d", task.Attempt)
	}
	if task.LastError == "" {
		t.Fatal("死信应记录错误")
	}
}

func TestVerifyCallbackRoundtrip(t *testing.T) {
	mock := newMockBot(t, 0, nil)
	d := &Dispatcher{}
	if err := d.VerifyCallback(mock.srv.URL, tok, aesK); err != nil {
		t.Fatalf("VerifyCallback: %v", err)
	}
	// 错误 Token 应失败
	if err := d.VerifyCallback(mock.srv.URL, "wrong-token", aesK); err == nil {
		t.Fatal("错误 Token 应验证失败")
	}
}
