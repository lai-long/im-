package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"im-/internal/core"
	"im-/internal/store"
)

// TestWebhookSend 覆盖 webhook/send 主流程与错误码。
func TestWebhookSend(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var got atomic.Int32
	csvc := core.New(st, func(core.Event) { got.Add(1) })
	mux := http.NewServeMux()
	RegisterWebhook(mux, csvc, st)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chat, _, _ := st.GetChatByWebhookKey(store.SeedWebhookKey)
	okURL := srv.URL + "/cgi-bin/webhook/send?key=" + store.SeedWebhookKey

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

	cases := []struct {
		name, url, body string
		wantErrcode     int
	}{
		{"text 正常", okURL, `{"msgtype":"text","text":{"content":"hello"}}`, 0},
		{"markdown 正常", okURL, `{"msgtype":"markdown","markdown":{"content":"# hi"}}`, 0},
		{"无效 key", srv.URL + "/cgi-bin/webhook/send?key=nope", `{"msgtype":"text","text":{"content":"x"}}`, 93000},
		{"缺失 key", srv.URL + "/cgi-bin/webhook/send", `{"msgtype":"text","text":{"content":"x"}}`, 93000},
		{"不支持 msgtype", okURL, `{"msgtype":"image","image":{"base64":"x"}}`, 40058},
		{"content 为空", okURL, `{"msgtype":"text","text":{"content":""}}`, 40008},
		{"JSON 非法", okURL, `{"msgtype":`, 40035},
		{"text 超长", okURL, `{"msgtype":"text","text":{"content":"` + strings.Repeat("a", 2049) + `"}}`, 45002},
	}
	for _, c := range cases {
		got := post(c.url, c.body)
		if int(got["errcode"].(float64)) != c.wantErrcode {
			t.Fatalf("%s: errcode=%v want %d (errmsg=%v)", c.name, got["errcode"], c.wantErrcode, got["errmsg"])
		}
	}

	msgs, _ := st.ListMessages(chat.ID, 50)
	if len(msgs) != 2 {
		t.Fatalf("期望 2 条机器人消息, got %d", len(msgs))
	}
	if got.Load() != 2 {
		t.Fatalf("期望广播 2 次, got %d", got.Load())
	}
}

// TestResponseURL 覆盖 response_url 主动回复：落群、引用、一次性与过期。
func TestResponseURL(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	csvc := core.New(st, nil)
	mux := http.NewServeMux()
	RegisterResponse(mux, csvc, st)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chat, bot, _ := st.GetChatByWebhookKey(store.SeedWebhookKey)
	u, _ := st.GetUserByUserid("zhangsan")
	msg, _, err := csvc.UserMessage(chat.ID, u.ID, "@示例机器人 在吗")
	if err != nil {
		t.Fatal(err)
	}

	// 一条有效 code（1 小时）与一条过期 code
	task, err := st.CreateCallbackTask(msg.ID, bot.ID, "bot", "{}", "code-ok",
		time.Now().Add(time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCallbackTask(msg.ID, bot.ID, "bot", "{}", "code-expired",
		time.Now().Add(-time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}

	post := func(code, body string) map[string]any {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/cgi-bin/aibot/response?response_code="+code,
			strings.NewReader(body))
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

	md := `{"msgtype":"markdown","markdown":{"content":"**在的**"}}`
	if out := post(task.ResponseCode, md); out["errcode"] != 0.0 {
		t.Fatalf("首次回复应成功: %v", out)
	}
	if out := post(task.ResponseCode, md); out["errcode"] != 40001.0 {
		t.Fatalf("重复使用应返回 40001: %v", out)
	}
	if out := post("code-expired", md); out["errcode"] != 40001.0 {
		t.Fatalf("过期 code 应返回 40001: %v", out)
	}
	if out := post(task.ResponseCode, `{"msgtype":"image"}`); out["errcode"] != 40001.0 {
		t.Fatalf("已用 code 应优先返回 40001: %v", out)
	}

	msgs, _ := st.ListMessages(chat.ID, 50)
	var reply store.Message
	for _, m := range msgs {
		if m.MsgType == "markdown" {
			reply = m
		}
	}
	if reply.ID == 0 {
		t.Fatal("主动回复未落群")
	}
	if reply.Content["content"] != "**在的**" {
		t.Fatalf("回复内容不符: %v", reply.Content)
	}
	quote, ok := reply.Content["quote"].(map[string]any)
	if !ok || quote["msgid"] != msg.Msgid || quote["sender"] != "张三" {
		t.Fatalf("引用不符: %v", reply.Content["quote"])
	}
	if quote["content"] != "@示例机器人 在吗" {
		t.Fatalf("引用内容不符: %v", quote)
	}
}

// TestResponseURLTemplateCard 覆盖 response_url 的 template_card 回复。
func TestResponseURLTemplateCard(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	csvc := core.New(st, nil)
	mux := http.NewServeMux()
	RegisterResponse(mux, csvc, st)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chat, bot, _ := st.GetChatByWebhookKey(store.SeedWebhookKey)
	u, _ := st.GetUserByUserid("zhangsan")
	msg, _, _ := csvc.UserMessage(chat.ID, u.ID, "@示例机器人 构建结果")
	task, _ := st.CreateCallbackTask(msg.ID, bot.ID, "bot", "{}", "code-card", time.Now().Add(time.Hour).Unix())

	post := func(body string) map[string]any {
		req, _ := http.NewRequest(http.MethodPost,
			srv.URL+"/cgi-bin/aibot/response?response_code="+task.ResponseCode, strings.NewReader(body))
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

	if out := post(`{"msgtype":"template_card","template_card":{"card_type":"text_notice","main_title":{"title":"构建成功"}}}`); out["errcode"] != 0.0 {
		t.Fatalf("template_card 回复应成功: %v", out)
	}
	if out := post(`{"msgtype":"template_card","template_card":{}}`); out["errcode"] != 40001.0 {
		t.Fatalf("复用 code 应返回 40001: %v", out)
	}

	msgs, _ := st.ListMessages(chat.ID, 50)
	for _, m := range msgs {
		if m.MsgType == "template_card" {
			if m.Content["card_type"] != "text_notice" {
				t.Fatalf("卡片内容不符: %v", m.Content)
			}
			if _, ok := m.Content["quote"]; !ok {
				t.Fatalf("卡片应带引用: %v", m.Content)
			}
			return
		}
	}
	t.Fatal("template_card 未落群")
}

// TestUploadMedia 覆盖 webhook/upload_media 与 file 消息发送。
func TestUploadMedia(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	csvc := core.New(st, nil)
	mux := http.NewServeMux()
	RegisterWebhook(mux, csvc, st)
	RegisterUploadMedia(mux, st)
	mux.HandleFunc("GET /api/media/{media_id}", func(w http.ResponseWriter, r *http.Request) {
		serveMedia(w, st, r.PathValue("media_id"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	base := srv.URL + "/cgi-bin/webhook/upload_media?key=" + store.SeedWebhookKey

	// 类型非法
	code, _ := uploadFile(t, base+"&type=image", "a.txt", []byte("x"))
	if code != 40004 {
		t.Fatalf("非法类型应返回 40004, got %d", code)
	}

	// 正常上传
	payload := []byte("hello media")
	code, out := uploadFile(t, base+"&type=file", "note.txt", payload)
	if code != 0 {
		t.Fatalf("上传应成功: %v", out)
	}
	mediaID, _ := out["media_id"].(string)
	if mediaID == "" {
		t.Fatalf("缺少 media_id: %v", out)
	}

	// 下载内容一致
	resp, err := http.Get(srv.URL + "/api/media/" + mediaID)
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, len(payload))
	n, _ := resp.Body.Read(body)
	resp.Body.Close()
	if string(body[:n]) != "hello media" {
		t.Fatalf("下载内容不符: %q", body[:n])
	}

	// 以 file 消息发送
	sendCode, msg := postJSONCode(srv.URL+"/cgi-bin/webhook/send?key="+store.SeedWebhookKey,
		fmt.Sprintf(`{"msgtype":"file","file":{"media_id":%q}}`, mediaID))
	if sendCode != 0 {
		t.Fatalf("file 消息发送失败: %s", msg)
	}
	// 不存在的 media_id
	badCode, _ := postJSONCode(srv.URL+"/cgi-bin/webhook/send?key="+store.SeedWebhookKey,
		`{"msgtype":"file","file":{"media_id":"nope"}}`)
	if badCode != 40007 {
		t.Fatalf("未知 media_id 应返回 40007, got %d", badCode)
	}
}

// uploadFile 以 multipart 上传文件，返回 errcode 与响应。
func uploadFile(t *testing.T, url, filename string, content []byte) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("media", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if v, ok := out["errcode"].(float64); ok {
		return int(v), out
	}
	return -1, out
}

// postJSONCode 发送 JSON POST，返回 errcode 与 errmsg。
func postJSONCode(url, body string) (int, string) {
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
