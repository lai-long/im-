// Package selftest 是回环自测：进程内启动平台 + mock 接入方，跑完 M1a + M1b + M2 验收全链路。
// 接入方侧使用企微官方加解密库（sbzhu/weworkapi_golang）实现，用于跨实现互通验证；
// 平台侧走真实 HTTP 路由，覆盖 docs/方案文档.md §7 的验收标准 1–6。
package selftest

import (
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
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
	mock := newMockReceiver(bot.CallbackToken, bot.CallbackAESKey, true) // 流式模式：覆盖 M1b 流式轮询

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
	add("URL 验证握手", srv.Dispatcher.VerifyCallback(mock.URL, bot.CallbackToken, bot.CallbackAESKey, "") == nil, "echostr 解密回显")

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
	userMsg, _, err := srv.Core.UserMessage(chat.ID, u.ID, "@"+bot.Name+" ping")
	if err != nil {
		add("用户 @bot 发消息", false, err.Error())
	} else {
		add("用户 @bot 发消息", true, "已生成回调任务")
		// 流式：首轮 finish=false → 平台轮询 → 最终 finish=true（覆盖 M1b）
		final, ok := waitForStreamFinal(chat.ID, st, 5*time.Second)
		add("流式回复落群（finish=true）", ok, final)
		add("流式刷新为同一条消息", countMessages(st, chat.ID, "stream") == 1,
			fmt.Sprintf("stream 消息数=%d（覆盖式刷新）", countMessages(st, chat.ID, "stream")))
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

	// 5. template_card 主动回复（response_url）
	// 上一步的 code 已占用，新建一条任务验证卡片回复
	cardCode := store.NewRandomString(24)
	{
		tk, err := st.CreateCallbackTask(userMsg.ID, bot.ID, "bot", "{}", cardCode, time.Now().Add(time.Hour).Unix())
		if err != nil {
			add("template_card 主动回复", false, err.Error())
		} else {
			c, m := postJSON(plat.URL+"/cgi-bin/aibot/response?response_code="+url.QueryEscape(tk.ResponseCode),
				`{"msgtype":"template_card","template_card":{"card_type":"text_notice","main_title":{"title":"构建成功"}}}`)
			add("template_card 主动回复", c == 0, m)
			add("template_card 落群", countMessages(st, chat.ID, "template_card") == 1, "卡片消息数=1")
		}
	}

	// 6. 自建应用：gettoken → message/send → XML 回调（M2）
	corp, err := st.FirstCorp()
	if err != nil {
		add("自建应用 gettoken", false, err.Error())
	} else {
		agent, aerr := st.GetAgent(1)
		if aerr != nil {
			add("自建应用 gettoken", false, aerr.Error())
		} else {
			// gettoken：真实 HTTP 路由
			var tr struct {
				Errcode     int    `json:"errcode"`
				AccessToken string `json:"access_token"`
				ExpiresIn   int    `json:"expires_in"`
			}
			req, _ := http.NewRequest(http.MethodGet,
				plat.URL+"/cgi-bin/gettoken?corpid="+corp.CorpID+"&corpsecret="+url.QueryEscape(agent.Corpsecret), nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				add("自建应用 gettoken", false, err.Error())
			} else {
				_ = json.NewDecoder(resp.Body).Decode(&tr)
				resp.Body.Close()
				add("自建应用 gettoken", tr.Errcode == 0 && tr.AccessToken != "", fmt.Sprintf("expires_in=%d", tr.ExpiresIn))

				// message/send → 应用单聊会话
				body := fmt.Sprintf(`{"touser":"zhangsan","msgtype":"text","agentid":%d,"text":{"content":"selftest 应用消息"}}`, agent.Agentid)
				code, msg := postJSON(plat.URL+"/cgi-bin/message/send?access_token="+tr.AccessToken, body)
				add("自建应用 message/send", code == 0, msg)

				// 单聊会话已创建且消息可见
				u, _ := st.GetUserByUserid("zhangsan")
				chats, _ := st.ChatsOfUser(u.ID)
				var direct *store.Chat
				for i := range chats {
					if chats[i].Type == "direct" {
						direct = &chats[i]
					}
				}
				add("自建应用单聊会话", direct != nil, fmt.Sprintf("用户会话数=%d", len(chats)))

				// XML 回调：配置接入方（官方库实现）并完成握手
				xmlMock := newXMLReceiver(agent.CallbackToken, agent.CallbackAES, corp.CorpID)
				_ = st.UpdateAgentCallback(agent.ID, xmlMock.URL, "encrypted")
				if err := srv.Dispatcher.VerifyCallback(xmlMock.URL, agent.CallbackToken, agent.CallbackAES, corp.CorpID); err != nil {
					add("自建应用回调握手", false, err.Error())
				} else {
					add("自建应用回调握手", true, "echostr 解密回显")
					if direct != nil {
						if _, _, err := srv.Core.UserMessage(direct.ID, u.ID, "查一下订单"); err != nil {
							add("自建应用 XML 回调", false, err.Error())
						} else {
							ok := waitUntil(func() bool { return xmlMock.received.Load() == 1 }, 5*time.Second)
							add("自建应用 XML 回调", ok, "官方库解密成功")
						}
					}
				}
			}
			_ = resp
		}
	}

	// 6b. 机器人单聊（chattype=single）+ 进入会话事件欢迎语（M2）
	u, _ = st.GetUserByUserid("zhangsan")
	singleMock := newMockReceiver(bot.CallbackToken, bot.CallbackAESKey, false)
	_ = st.UpdateBotCallback(bot.ID, singleMock.URL, bot.CallbackToken, bot.CallbackAESKey, "encrypted")
	singleChat, created, err := st.OpenBotSingleChat(bot.ID, u.ID, bot.Name)
	if err != nil || !created {
		add("机器人单聊·创建会话", false, err.Error())
	} else {
		add("机器人单聊·创建会话", true, "chattype=single")
		srv.Dispatcher.EnqueueBotEntry(singleChat, bot, u)
		ok := waitUntil(func() bool { return countMessages(st, singleChat.ID, "stream") >= 1 }, 5*time.Second)
		add("机器人单聊·进入会话欢迎语", ok, "event 回调 → 被动回复落单聊")
		if _, _, err := srv.Core.UserMessage(singleChat.ID, u.ID, "在吗"); err != nil {
			add("机器人单聊·用户发言触发回调", false, err.Error())
		} else {
			ok2 := waitUntil(func() bool { return countMessages(st, singleChat.ID, "stream") >= 2 }, 5*time.Second)
			add("机器人单聊·被动回复落群", ok2, "chattype=single 无 chatid")
		}
	}

	// 6c. webhook/send 扩展消息类型（image / news / template_card）
	for _, tc := range []struct{ mt, body string }{
		{"image", `{"msgtype":"image","image":{"base64":"aGVsbG8=","md5":"x"}}`},
		{"news", `{"msgtype":"news","news":{"articles":[{"title":"t","description":"d","url":"http://x","picurl":""}]}}`},
		{"template_card", `{"msgtype":"template_card","template_card":{"card_type":"text_notice","main_title":{"title":"构建"}}}`},
	} {
		code, msg := postJSON(plat.URL+"/cgi-bin/webhook/send?key="+store.SeedWebhookKey, tc.body)
		add("webhook/send "+tc.mt, code == 0, msg)
	}
	okImg := waitUntil(func() bool {
		return countMessages(st, chat.ID, "image") == 1 &&
			countMessages(st, chat.ID, "news") == 1 &&
			countMessages(st, chat.ID, "template_card") >= 1
	}, 3*time.Second)
	add("webhook 新类型落群", okImg, "image/news/template_card 已落库")

	// 6d. 通讯录只读（M2）：user/get、user/simplelist
	if corp.CorpID != "" {
		if agent2, aerr := st.GetAgent(1); aerr == nil {
			gt := getJSON(plat.URL + "/cgi-bin/gettoken?corpid=" + corp.CorpID + "&corpsecret=" + url.QueryEscape(agent2.Corpsecret))
			if tk, ok := gt["access_token"].(string); ok && tk != "" {
				cug := getJSON(plat.URL + "/cgi-bin/user/get?access_token=" + tk + "&userid=zhangsan")
				add("通讯录 user/get", cug["errcode"] == 0.0 && cug["name"] == "张三", "userid→姓名")
				csl := getJSON(plat.URL + "/cgi-bin/user/simplelist?access_token=" + tk)
				if lst, ok := csl["userlist"].([]any); ok {
					add("通讯录 user/simplelist", csl["errcode"] == 0.0 && len(lst) > 0, "用户列表非空")
				} else {
					add("通讯录 user/simplelist", false, "userlist 缺失")
				}
			} else {
				add("通讯录 user/get", false, "gettoken 失败")
			}
		}
	}

	// 7. TLS 自签证书（M1b）
	certFile, keyFile, err := server.EnsureSelfSignedCert(dir)
	if err != nil {
		add("TLS 自签证书", false, err.Error())
	} else if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		add("TLS 自签证书", false, err.Error())
	} else {
		add("TLS 自签证书", true, "生成并可加载（数据目录复用）")
	}

	// 输出
	fmt.Println("\n=== 回环自测（M1a + M1b + M2 验收全链路）===")
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

// getJSON 发送 GET 并解析整个 JSON 响应。
func getJSON(url string) map[string]any {
	resp, err := http.Get(url)
	if err != nil {
		return map[string]any{"errcode": -1, "errmsg": err.Error()}
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
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
	URL        string
	crypt      *official.WXBizMsgCrypt
	payloadOK  atomic.Int32
	pushes     atomic.Int32
	streamMode bool // true：首轮 finish=false，刷新轮 finish=true（验证流式轮询）
}

func newMockReceiver(token, aesKey string, streamMode bool) *mockReceiver {
	m := &mockReceiver{crypt: official.NewWXBizMsgCrypt(token, aesKey, "", official.XmlType), streamMode: streamMode}
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
		// 被动回复：一次性 stream（finish=true）；streamMode 下首轮 finish=false 触发轮询
		n := m.pushes.Add(1)
		finish := true
		content := "**pong**"
		if m.streamMode && n == 1 {
			finish, content = false, "思考中…"
		}
		reply := map[string]any{"msgtype": "stream",
			"stream": map[string]any{"id": "selftest", "finish": finish, "content": content}}
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

// waitForStreamFinal 等待流式消息刷到 finish=true，返回最终内容。
func waitForStreamFinal(chatID int64, st *store.Store, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msgs, _ := st.ListMessages(chatID, 200)
		for _, m := range msgs {
			if m.MsgType == "stream" {
				if fin, ok := m.Content["finish"].(bool); ok && fin {
					b, _ := json.Marshal(m.Content)
					return string(b), true
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "", false
}

// countMessages 统计指定类型的消息条数。
func countMessages(st *store.Store, chatID int64, msgType string) int {
	msgs, _ := st.ListMessages(chatID, 200)
	n := 0
	for _, m := range msgs {
		if m.MsgType == msgType {
			n++
		}
	}
	return n
}

// waitUntil 轮询等待条件成立。
func waitUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// xmlReceiver 模拟"自建应用接入方"：用官方库验签解密 XML 回调（receiveid=corpid）。
type xmlReceiver struct {
	URL      string
	crypt    *official.WXBizMsgCrypt
	received atomic.Int32
}

func newXMLReceiver(token, aesKey, corpid string) *xmlReceiver {
	m := &xmlReceiver{crypt: official.NewWXBizMsgCrypt(token, aesKey, corpid, official.XmlType)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method == http.MethodGet {
			plain, cerr := m.crypt.VerifyURL(q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce"), q.Get("echostr"))
			if cerr != nil {
				http.Error(w, "verify fail", http.StatusBadRequest)
				return
			}
			_, _ = w.Write(plain)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if _, cerr := m.crypt.DecryptMsg(q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce"), body); cerr != nil {
			http.Error(w, "decrypt fail", http.StatusBadRequest)
			return
		}
		m.received.Store(1)
		_, _ = w.Write([]byte("success"))
	}))
	m.URL = srv.URL
	return m
}
