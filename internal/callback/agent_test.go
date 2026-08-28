package callback

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	official "github.com/sbzhu/weworkapi_golang/wxbizmsgcrypt"

	"im-/internal/core"
	"im-/internal/store"
)

// TestAgentXMLCallback 覆盖自建应用链路：
// 用户在"用户↔应用"单聊发消息 → 平台以加密 XML 回调（receiveid=corpid）推送 →
// 官方库验签解密 → 接入方被动回复（明文 XML）→ 回复落回单聊。
func TestAgentXMLCallback(t *testing.T) {
	st, _, _ := setup(t)
	agent, err := st.GetAgent(1)
	if err != nil {
		t.Fatal(err)
	}
	corp, err := st.FirstCorp()
	if err != nil {
		t.Fatal(err)
	}

	var pushed atomicString
	cpt := official.NewWXBizMsgCrypt(agent.CallbackToken, agent.CallbackAES, corp.CorpID, official.XmlType)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method == http.MethodGet {
			plain, cerr := cpt.VerifyURL(q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce"), q.Get("echostr"))
			if cerr != nil {
				http.Error(w, "verify fail", http.StatusBadRequest)
				return
			}
			_, _ = w.Write(plain)
			return
		}
		// 官方库直接消费加密 XML 信封
		body, _ := readAll(r)
		msg, cerr := cpt.DecryptMsg(q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce"), body)
		if cerr != nil {
			http.Error(w, "decrypt fail", http.StatusBadRequest)
			return
		}
		pushed.set(string(msg))
		// 被动回复：明文 XML
		_, _ = w.Write([]byte(`<xml><ToUserName><![CDATA[` + corp.CorpID + `]]></ToUserName>` +
			`<FromUserName><![CDATA[sys]]></FromUserName><CreateTime>12345678</CreateTime>` +
			`<MsgType><![CDATA[text]]></MsgType><Content><![CDATA[已收到你的消息]]></Content></xml>`))
	}))
	defer srv.Close()
	if err := st.UpdateAgentCallback(agent.ID, srv.URL, "encrypted"); err != nil {
		t.Fatal(err)
	}

	csvc := core.New(st, nil)
	d := NewDispatcher(st, "http://127.0.0.1:7788", csvc)
	d.Backoff = []time.Duration{5 * time.Millisecond}
	csvc.OnAgentDirect = func(m store.Message, agentID int64) {
		a, err := st.GetAgent(agentID)
		if err != nil {
			return
		}
		d.EnqueueAgentMessage(m, a)
	}
	d.Start()
	defer d.Stop()

	// 用户在应用单聊发消息
	u, _ := st.GetUserByUserid("zhangsan")
	chat, err := st.CreateDirectChat(agent.ID, u.ID, agent.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := csvc.UserMessage(chat.ID, u.ID, "查一下今天的订单"); err != nil {
		t.Fatal(err)
	}

	task := waitTaskStatus(t, st, "done")
	xml := pushed.get()
	if !strings.Contains(xml, "<MsgType><![CDATA[text]]></MsgType>") {
		t.Fatalf("回调 XML 结构不符: %s", xml)
	}
	if !strings.Contains(xml, "<Content><![CDATA[查一下今天的订单]]></Content>") {
		t.Fatalf("回调 XML 内容不符: %s", xml)
	}
	if !strings.Contains(xml, "<AgentID>1000002</AgentID>") {
		t.Fatalf("回调 XML 缺 AgentID: %s", xml)
	}
	if !strings.Contains(xml, "<FromUserName><![CDATA[zhangsan]]></FromUserName>") {
		t.Fatalf("回调 XML 缺发送者: %s", xml)
	}

	// 被动回复落回同一单聊
	msgs, _ := st.ListMessages(chat.ID, 50)
	for _, m := range msgs {
		if m.SenderTyp == "agent" && m.Content["content"] == "已收到你的消息" {
			_ = task
			return
		}
	}
	t.Fatalf("被动回复未落回单聊: %+v", msgs)
}

func readAll(r *http.Request) ([]byte, error) {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}

// atomicString 简单并发安全字符串容器（测试用）。
type atomicString struct {
	mu  chan struct{}
	val string
}

func (a *atomicString) set(v string) {
	if a.mu == nil {
		a.mu = make(chan struct{}, 1)
	}
	a.mu <- struct{}{}
	a.val = v
	<-a.mu
}

func (a *atomicString) get() string {
	if a.mu == nil {
		return ""
	}
	a.mu <- struct{}{}
	defer func() { <-a.mu }()
	return a.val
}
