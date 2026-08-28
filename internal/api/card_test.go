package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"im-/internal/callback"
	"im-/internal/core"
	"im-/internal/store"
)

// TestCardInteract 覆盖模板卡片交互闭环：点击按钮 → template_card_event 回调任务 →
// 接入方用 response_code 调 update_template_card 原地替换卡片内容。
func TestCardInteract(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	csvc := core.New(st, func(core.Event) {})
	disp := callback.NewDispatcher(st, "http://127.0.0.1:7788", csvc)

	mux := http.NewServeMux()
	RegisterWebhook(mux, csvc, st)
	RegisterCardAPI(mux, csvc, st, disp)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chat, _, _ := st.GetChatByWebhookKey(store.SeedWebhookKey)

	post := func(url, body string) map[string]any {
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	// 1. 发一张带按钮的模板卡片
	okURL := srv.URL + "/cgi-bin/webhook/send?key=" + store.SeedWebhookKey
	r := post(okURL, `{"msgtype":"template_card","template_card":{"card_type":"button_interaction",
		"main_title":{"title":"构建"},"button_list":[{"text":"确认","key":"btn_ok"}]}}`)
	if r["errcode"].(float64) != 0 {
		t.Fatalf("webhook 发卡片失败: %v", r)
	}
	msgs, _ := st.ListMessages(chat.ID, 20)
	var cardMsg store.Message
	for _, m := range msgs {
		if m.MsgType == "template_card" {
			cardMsg = m
		}
	}
	if cardMsg.Msgid == "" {
		t.Fatal("未找到卡片消息")
	}

	// 2. 用户点击按钮 → 交互事件
	r = post(srv.URL+"/api/card/interact",
		`{"userid":"zhangsan","msgid":"`+cardMsg.Msgid+`","event_key":"btn_ok"}`)
	if r["errcode"].(float64) != 0 {
		t.Fatalf("interact 失败: %v", r)
	}
	tasks, _ := st.ListCallbackTasks("", 10)
	var found bool
	var code string
	for _, tk := range tasks {
		if strings.Contains(tk.Payload, `"event":"template_card_event"`) && tk.MessageID == cardMsg.ID {
			found, code = true, tk.ResponseCode
		}
	}
	if !found {
		t.Fatal("未创建 template_card_event 回调任务")
	}

	// 3. 接入方调 update_template_card 更新原卡片
	r = post(srv.URL+"/cgi-bin/aibot/update_template_card?response_code="+code,
		`{"template_card":{"card_type":"text_notice","main_title":{"title":"已确认 ✅"}}}`)
	if r["errcode"].(float64) != 0 {
		t.Fatalf("update_template_card 失败: %v", r)
	}
	updated, _ := st.GetMessageByMsgid(cardMsg.Msgid)
	if mt, ok := updated.Content["main_title"].(map[string]any); !ok || mt["title"] != "已确认 ✅" {
		t.Fatalf("卡片内容未更新: %v", updated.Content)
	}

	// 4. 错误码：缺失/非法 response_code
	r = post(srv.URL+"/cgi-bin/aibot/update_template_card",
		`{"template_card":{"main_title":{"title":"x"}}}`)
	if r["errcode"].(float64) != 40001 {
		t.Fatalf("缺 response_code 应 40001, got %v", r)
	}
	r = post(srv.URL+"/cgi-bin/aibot/update_template_card?response_code=nope",
		`{"template_card":{"main_title":{"title":"x"}}}`)
	if r["errcode"].(float64) != 40001 {
		t.Fatalf("非法 response_code 应 40001, got %v", r)
	}
	r = post(srv.URL+"/cgi-bin/aibot/update_template_card?response_code="+code, `{"template_card":{}}`)
	if r["errcode"].(float64) != 40008 {
		t.Fatalf("空卡片应 40008, got %v", r)
	}

	// 5. 非卡片消息交互 → 拒绝（HTTP 400，无 errcode=0）
	txt, _ := csvc.BotMessage(chat.ID, 1, "text", map[string]any{"content": "hi"}, nil)
	r = post(srv.URL+"/api/card/interact",
		`{"userid":"zhangsan","msgid":"`+txt.Msgid+`","event_key":"k"}`)
	if ec, ok := r["errcode"]; ok && ec == 0.0 {
		t.Fatal("非卡片消息交互不应成功")
	}
}
