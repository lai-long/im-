package callback

import (
	"encoding/json"
	"encoding/xml"
	"io"
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

// streamingBot 模拟"流式"接入方：第一轮 finish=false，后续轮次逐段追加内容，
// 最后一轮 finish=true。每轮应答都用官方库加密，平台侧独立解密。
type streamingBot struct {
	srv   *httptest.Server
	crypt *official.WXBizMsgCrypt
	round atomic.Int32
	total int32 // 结束所需轮次（含第 1 轮被动回复）
}

func newStreamingBot(t *testing.T, total int32) *streamingBot {
	b := &streamingBot{crypt: official.NewWXBizMsgCrypt(tok, aesK, "", official.XmlType), total: total}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method == http.MethodGet {
			plain, cerr := b.crypt.VerifyURL(q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce"), q.Get("echostr"))
			if cerr != nil {
				http.Error(w, "bad", http.StatusBadRequest)
				return
			}
			_, _ = w.Write(plain)
			return
		}
		// 解密推送（首轮回调与流式刷新回调同一路径）
		var env struct {
			Encrypt string `json:"encrypt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&env)
		if _, cerr := b.crypt.DecryptMsg(q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce"),
			[]byte("<xml><Encrypt><![CDATA["+env.Encrypt+"]]></Encrypt></xml>")); cerr != nil {
			http.Error(w, "decrypt fail", http.StatusBadRequest)
			return
		}
		n := b.round.Add(1)
		reply := map[string]any{"msgtype": "stream",
			"stream": map[string]any{
				"id":      "stream-1",
				"finish":  n >= b.total,
				"content": strings.Repeat("段", int(n)),
			}}
		raw, _ := b.crypt.EncryptMsg(mustJSON(reply), q.Get("timestamp"), q.Get("nonce"))
		var out struct {
			Encrypt string `xml:"Encrypt"`
		}
		_ = xml.Unmarshal(raw, &out)
		_ = json.NewEncoder(w).Encode(map[string]string{"encrypt": out.Encrypt})
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func TestStreamingRefresh(t *testing.T) {
	st, chat, bot := setup(t)
	mock := newStreamingBot(t, 3) // 3 轮后结束
	if err := st.UpdateBotCallback(bot.ID, mock.srv.URL, tok, aesK, "encrypted"); err != nil {
		t.Fatal(err)
	}

	csvc := core.New(st, nil)
	d := NewDispatcher(st, "http://127.0.0.1:7788", csvc)
	d.StreamTick = 5 * time.Millisecond
	csvc.OnBotMention = d.EnqueueUserMessage
	d.Start()
	defer d.Stop()

	u, _ := st.GetUserByUserid("zhangsan")
	if _, _, err := csvc.UserMessage(chat.ID, u.ID, "@示例机器人 流式测试"); err != nil {
		t.Fatal(err)
	}

	task := waitTaskStatus(t, st, "done")
	if mock.round.Load() != 3 {
		t.Fatalf("期望 3 轮推送, got %d", mock.round.Load())
	}
	if !strings.Contains(task.LastError, "流式回复完成") {
		t.Fatalf("任务备注应记录流式完成, got %q", task.LastError)
	}

	// 流式消息：始终同一条（全量刷新覆盖），内容为最终轮次的完整内容
	msgs, _ := st.ListMessages(chat.ID, 200)
	var streamMsgs []store.Message
	for _, m := range msgs {
		if m.MsgType == "stream" {
			streamMsgs = append(streamMsgs, m)
		}
	}
	if len(streamMsgs) != 1 {
		t.Fatalf("流式刷新应只产生 1 条消息（覆盖式）, got %d", len(streamMsgs))
	}
	if streamMsgs[0].Content["content"] != "段段段" {
		t.Fatalf("最终内容不符: %v", streamMsgs[0].Content["content"])
	}
	if streamMsgs[0].Content["finish"] != true {
		t.Fatalf("最终 finish 应为 true: %v", streamMsgs[0].Content["finish"])
	}
}

func TestStreamingWindowEnds(t *testing.T) {
	st, chat, bot := setup(t)
	// 接入方永远不 finish：应由 6 分钟窗口（此处注入 30ms）结束
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cpt := official.NewWXBizMsgCrypt(tok, aesK, "", official.XmlType)
		q := r.URL.Query()
		var env struct {
			Encrypt string `json:"encrypt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&env)
		if _, cerr := cpt.DecryptMsg(q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce"),
			[]byte("<xml><Encrypt><![CDATA["+env.Encrypt+"]]></Encrypt></xml>")); cerr != nil {
			http.Error(w, "decrypt fail", http.StatusBadRequest)
			return
		}
		reply := map[string]any{"msgtype": "stream",
			"stream": map[string]any{"id": "s", "finish": false, "content": "thinking"}}
		raw, _ := cpt.EncryptMsg(mustJSON(reply), q.Get("timestamp"), q.Get("nonce"))
		var out struct {
			Encrypt string `xml:"Encrypt"`
		}
		_ = xml.Unmarshal(raw, &out)
		_ = json.NewEncoder(w).Encode(map[string]string{"encrypt": out.Encrypt})
	}))
	defer srv.Close()
	if err := st.UpdateBotCallback(bot.ID, srv.URL, tok, aesK, "encrypted"); err != nil {
		t.Fatal(err)
	}

	csvc := core.New(st, nil)
	d := NewDispatcher(st, "http://127.0.0.1:7788", csvc)
	d.StreamTick = 5 * time.Millisecond
	d.StreamWindow = 30 * time.Millisecond
	csvc.OnBotMention = d.EnqueueUserMessage
	d.Start()
	defer d.Stop()

	u, _ := st.GetUserByUserid("lisi")
	if _, _, err := csvc.UserMessage(chat.ID, u.ID, "@示例机器人 长任务"); err != nil {
		t.Fatal(err)
	}

	task := waitTaskStatus(t, st, "done")
	if !strings.Contains(task.LastError, "流式窗口结束") {
		t.Fatalf("期望窗口结束时结束任务, got %q", task.LastError)
	}
}

func TestTemplateCardPassiveReply(t *testing.T) {
	st, chat, bot := setup(t)
	card := map[string]any{"card_type": "text_notice",
		"main_title":     map[string]any{"title": "构建完成"},
		"sub_title_text": "全部用例通过"}
	mock := newMockBot(t, 0, func() map[string]any {
		return map[string]any{"msgtype": "template_card", "template_card": card}
	})
	if err := st.UpdateBotCallback(bot.ID, mock.srv.URL, tok, aesK, "encrypted"); err != nil {
		t.Fatal(err)
	}

	csvc := core.New(st, nil)
	d := NewDispatcher(st, "http://127.0.0.1:7788", csvc)
	d.Backoff = []time.Duration{5 * time.Millisecond}
	csvc.OnBotMention = d.EnqueueUserMessage
	d.Start()
	defer d.Stop()

	u, _ := st.GetUserByUserid("zhangsan")
	if _, _, err := csvc.UserMessage(chat.ID, u.ID, "@示例机器人 发个卡片"); err != nil {
		t.Fatal(err)
	}

	waitTaskStatus(t, st, "done")
	msgs, _ := st.ListMessages(chat.ID, 200)
	for _, m := range msgs {
		if m.MsgType == "template_card" {
			if m.Content["card_type"] != "text_notice" {
				t.Fatalf("卡片内容不符: %v", m.Content)
			}
			return
		}
	}
	t.Fatal("template_card 未落群")
}

// TestPlainMode 覆盖明文回调模式：推送未加密 JSON、被动回复按明文解析。
func TestPlainMode(t *testing.T) {
	st, chat, bot := setup(t)
	var gotPayload atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 明文模式：不应带签名参数
		if r.URL.Query().Get("msg_signature") != "" {
			http.Error(w, "plain mode should not sign", http.StatusBadRequest)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var p map[string]any
		if err := json.Unmarshal(raw, &p); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		gotPayload.Store(string(raw))
		_, _ = w.Write([]byte(mustJSON(map[string]any{"msgtype": "stream",
			"stream": map[string]any{"id": "s", "finish": true, "content": "明文回复"}})))
	}))
	defer srv.Close()
	if err := st.UpdateBotCallback(bot.ID, srv.URL, tok, aesK, "plain"); err != nil {
		t.Fatal(err)
	}

	csvc := core.New(st, nil)
	d := NewDispatcher(st, "http://127.0.0.1:7788", csvc)
	csvc.OnBotMention = d.EnqueueUserMessage
	d.Start()
	defer d.Stop()

	u, _ := st.GetUserByUserid("zhangsan")
	if _, _, err := csvc.UserMessage(chat.ID, u.ID, "@示例机器人 明文模式"); err != nil {
		t.Fatal(err)
	}
	waitTaskStatus(t, st, "done")

	p, _ := gotPayload.Load().(string)
	if !strings.Contains(p, `"msgtype":"text"`) || !strings.Contains(p, `"chattype":"group"`) {
		t.Fatalf("明文推送内容不符: %s", p)
	}
	if strings.Contains(p, "encrypt") {
		t.Fatalf("明文模式不应包含 encrypt 字段: %s", p)
	}
	msgs, _ := st.ListMessages(chat.ID, 200)
	for _, m := range msgs {
		if m.MsgType == "stream" && m.Content["content"] == "明文回复" {
			return
		}
	}
	t.Fatal("明文被动回复未落群")
}
