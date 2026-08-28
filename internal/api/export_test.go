package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"im-/internal/core"
	"im-/internal/store"
	"im-/internal/ws"
)

// TestExportReplay 覆盖消息导出（json/csv）与会话回放。
func TestExportReplay(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	csvc := core.New(st, func(core.Event) {})
	hub := ws.NewHub()
	mux := http.NewServeMux()
	RegisterExportAPI(mux, st, hub)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chat, _, _ := st.GetChatByWebhookKey(store.SeedWebhookKey)
	if _, err := csvc.BotMessage(chat.ID, 1, "text", map[string]any{"content": "export me"}, nil); err != nil {
		t.Fatal(err)
	}

	// JSON 导出
	resp, err := http.Get(srv.URL + "/api/export?chat_id=" + fmt.Sprint(chat.ID))
	if err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(arr) == 0 {
		t.Fatal("JSON 导出为空")
	}

	// CSV 导出
	resp, err = http.Get(srv.URL + "/api/export?chat_id=" + fmt.Sprint(chat.ID) + "&format=csv")
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 1024)
	n, _ := resp.Body.Read(body)
	resp.Body.Close()
	if !strings.HasPrefix(string(body[:n]), "ts,sender") {
		t.Fatalf("CSV 缺表头: %s", string(body[:n]))
	}

	// 会话回放
	r := postJSONMap(srv.URL+"/api/replay",
		fmt.Sprintf(`{"userid":"zhangsan","chat_id":%d}`, chat.ID))
	if r["errcode"].(float64) != 0 {
		t.Fatalf("回放失败: %v", r)
	}

	// 缺参数
	resp, _ = http.Get(srv.URL + "/api/export")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺 chat_id 应 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
