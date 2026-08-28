// Package selftest 是回环自测：进程内启动平台 + mock 接入方，跑完 M1a 验收全链路。
// 接入方侧使用企微官方加解密库（sbzhu/weworkapi_golang）实现，用于跨实现互通验证；
// 平台侧走真实 HTTP 路由，覆盖 docs/方案文档.md §7 的验收标准 1–6。
package selftest

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	official "github.com/sbzhu/weworkapi_golang/wxbizmsgcrypt"

	"im-/internal/config"
	"im-/internal/server"
	"im-/internal/store"
)

// Check 是一项自测结果。
type Check struct {
	Name   string
	OK     bool
	Detail string
}

// Run 执行回环自测，返回退出码（0 全通过，1 有失败）。
func Run(dataDir string) int {
	dir, err := os.MkdirTemp(dataDir, "selftest-")
	if err != nil {
		dir = os.TempDir()
	}
	defer os.RemoveAll(dir)

	st, err := store.Open(dir)
	if err != nil {
		fmt.Printf("[FAIL] 初始化存储: %v\n", err)
		return 1
	}
	defer st.Close()

	// —— mock 接入方（官方加解密库实现，独立于平台实现） ——
	seed, err := st.SeedWebhookInfo()
	if err != nil {
		fmt.Printf("[FAIL] 读取示例机器人信息: %v\n", err)
		return 1
	}
	fmt.Printf("示例机器人: %s（webhook key %s）\n", seed.BotName, seed.WebhookKey)
	chat, bot, err := st.GetChatByWebhookKey(store.SeedWebhookKey)
	if err != nil {
		fmt.Printf("[FAIL] 读取示例机器人: %v\n", err)
		return 1
	}
	mock := newMockReceiver(bot.CallbackToken, bot.CallbackAESKey)

	// —— 平台：真实 HTTP 路由 + 回调分发器 ——
	cfg := &config.Config{Addr: "127.0.0.1:0", DataDir: dir}
	srv := server.New(cfg, st)
	srv.Start()
	plat := httptest.NewServer(srv.Handler())
	defer plat.Close()
	defer srv.Dispatcher.Stop()

	if err := st.UpdateBotCallback(bot.ID, mock.URL, bot.CallbackToken, bot.CallbackAESKey, "encrypted"); err != nil {
		fmt.Printf("[FAIL] 保存回调配置: %v\n", err)
		return 1
	}
	bot.CallbackURL = mock.URL

	var checks []Check
	add := func(name string, ok bool, detail string) {
		checks = append(checks, Check{name, ok, detail})
	}

	// 1. URL 验证握手
	add("URL 验证握手", srv.Dispatcher.VerifyCallback(mock.URL, bot.CallbackToken, bot.CallbackAESKey) == nil, "echostr 解密回显")

	// 2. webhook/send 正常与错误码
	code, msg := postJSON(plat.URL+"/cgi-bin/webhook/send?key="+store.SeedWebhookKey,
		`{"msgtype":"text","text":{"content":"selftest hello"}}`)
	add("webhook/send text", code == 0, msg)
	badCode, badMsg := postJSON(plat.URL+"/cgi-bin/webhook/send?key=bad",
		`{"msgtype":"text","text":{"content":"x"}}`)
	add("webhook/send 无效 key → 93000", badCode == 93000, badMsg)

	// 3. 用户 @bot → 回调推送 → 官方库解密 → 被动回复落群
	u, err := st.GetUserByUserid("zhangsan")
	if err != nil {
		fmt.Printf("[FAIL] 读取用户: %v\n", err)
		return 1
	}
	if _, _, err := srv.Core.UserMessage(chat.ID, u.ID, "@"+bot.Name+" ping"); err != nil {
		add("用户 @bot 发消息", false, err.Error())
	} else {
		add("用户 @bot 发消息", true, "已生成回调任务")
		reply, ok := waitFor(chat.ID, st, "stream", 5*time.Second)
		add("被动回复落群", ok, reply)
		add("回调 JSON 字段完整", mock.payloadOK.Load() == 1, "msgid/aibotid/chatid/chattype/from/response_url/msgtype/text")
	}

	// 4. response_url 一次性语义
	tasks, _ := st.ListCallbackTasks("done", 5)
	if len(tasks) == 0 {
		add("response_url 主动回复", false, "未找到回调任务")
	} else {
		rc := url.QueryEscape(tasks[len(tasks)-1].ResponseCode)
		c1, m1 := postJSON(plat.URL+"/cgi-bin/aibot/response?response_code="+rc,
			`{"msgtype":"markdown","markdown":{"content":"**pong via response_url**"}}`)
		add("response_url 主动回复", c1 == 0, m1)
		c2, m2 := postJSON(plat.URL+"/cgi-bin/aibot/response?response_code="+rc,
			`{"msgtype":"markdown","markdown":{"content":"again"}}`)
		add("response_url 一次性（复用 → 40001）", c2 == 40001, m2)
		_, ok := waitFor(chat.ID, st, "markdown", 3*time.Second)
		add("主动回复落群", ok, "markdown + 引用")
	}

	// 输出
	fmt.Println("\n=== 回环自测（M1a 验收全链路）===")
	failed := 0
	for _, c := range checks {
		status := "PASS"
		if !c.OK {
			status, failed = "FAIL", failed+1
		}
		fmt.Printf("[%s] %-34s %s\n", status, c.Name, c.Detail)
	}
	fmt.Printf("\n%d 项通过，%d 项失败\n", len(checks)-failed, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

func postJSON(url, body string) (int, string) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return -1, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return -1, err.Error()
	}
	defer resp.Body.Close()
	var out struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.Errcode, out.Errmsg
}

// waitFor 等待聊天内出现指定类型的消息，返回其内容摘要。
func waitFor(chatID int64, st *store.Store, msgType string, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msgs, _ := st.ListMessages(chatID, 200)
		for _, m := range msgs {
			if m.MsgType == msgType {
				b, _ := json.Marshal(m.Content)
				return string(b), true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "", false
}

// mockReceiver 模拟"接入方"服务端，用企微官方加解密库实现接收侧。
type mockReceiver struct {
	URL       string
	crypt     *official.WXBizMsgCrypt
	payloadOK atomic.Int32
}

func newMockReceiver(token, aesKey string) *mockReceiver {
	m := &mockReceiver{crypt: official.NewWXBizMsgCrypt(token, aesKey, "", official.XmlType)}
	srv := httptest.NewServer(http.HandlerFunc(m.handle))
	m.URL = srv.URL
	return m
}

func (m *mockReceiver) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch r.Method {
	case http.MethodGet:
		plain, cerr := m.crypt.VerifyURL(q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce"), q.Get("echostr"))
		if cerr != nil {
			http.Error(w, "verify fail", http.StatusBadRequest)
			return
		}
		_, _ = w.Write(plain)
	case http.MethodPost:
		var env struct {
			Encrypt string `json:"encrypt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&env)
		msg, cerr := m.crypt.DecryptMsg(q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce"),
			[]byte("<xml><Encrypt><![CDATA["+env.Encrypt+"]]></Encrypt></xml>"))
		if cerr != nil {
			http.Error(w, "decrypt fail", http.StatusBadRequest)
			return
		}
		if payloadFieldsOK(msg) {
			m.payloadOK.Store(1)
		}
		// 被动回复：一次性 stream（finish=true）
		reply := map[string]any{"msgtype": "stream",
			"stream": map[string]any{"id": "selftest", "finish": true, "content": "**pong**"}}
		raw, cerr := m.crypt.EncryptMsg(mustJSON(reply), q.Get("timestamp"), q.Get("nonce"))
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
}

// payloadFieldsOK 校验回调 JSON 字段是否与协议基线一致（设计文档 §7.4）。
func payloadFieldsOK(msg []byte) bool {
	var p struct {
		Msgid    string `json:"msgid"`
		Aibotid  string `json:"aibotid"`
		Chatid   string `json:"chatid"`
		Chattype string `json:"chattype"`
		From     struct {
			Userid string `json:"userid"`
		} `json:"from"`
		ResponseURL string `json:"response_url"`
		Msgtype     string `json:"msgtype"`
		Text        struct {
			Content string `json:"content"`
		} `json:"text"`
	}
	if err := json.Unmarshal(msg, &p); err != nil {
		return false
	}
	return p.Msgid != "" && p.Aibotid != "" && p.Chatid != "" && p.Chattype == "group" &&
		p.From.Userid != "" && p.ResponseURL != "" && p.Msgtype == "text" && p.Text.Content != ""
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
