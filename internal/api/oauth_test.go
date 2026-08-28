package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"im-/internal/core"
	"im-/internal/store"
)

// TestOAuth2AndAppchat 覆盖 OAuth2 网页授权闭环与应用群聊 appchat/send。
func TestOAuth2AndAppchat(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	csvc := core.New(st, func(core.Event) {})
	mux := http.NewServeMux()
	RegisterAgentAPI(mux, csvc, st)
	RegisterOAuthAPI(mux, csvc, st)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	corp, _ := st.FirstCorp()
	agent, _ := st.GetAgent(1)

	tok := tokenFor(t, srv.URL, corp.CorpID, agent.Corpsecret)
	if tok == "" {
		t.Fatal("gettoken 失败")
	}

	// 1. 授权跳转：302 + code + state
	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	authURL := srv.URL + "/cgi-bin/oauth2/authorize?appid=" + corp.CorpID +
		"&redirect_uri=" + url.QueryEscape("http://localhost/cb?from=x") + "&state=abc&userid=zhangsan"
	resp, err := cli.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("授权应 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	lu, _ := url.Parse(loc)
	if lu == nil || lu.Query().Get("code") == "" || lu.Query().Get("state") != "abc" || lu.Query().Get("from") != "x" {
		t.Fatalf("回跳地址异常: %s", loc)
	}
	code := lu.Query().Get("code")

	// 2. code 换 userid；复用 → 40163
	gui := getJSON(srv.URL + "/cgi-bin/user/getuserinfo?access_token=" + tok + "&code=" + url.QueryEscape(code))
	if gui["errcode"].(float64) != 0 || gui["userid"] != "zhangsan" {
		t.Fatalf("getuserinfo 异常: %v", gui)
	}
	gui2 := getJSON(srv.URL + "/cgi-bin/user/getuserinfo?access_token=" + tok + "&code=" + url.QueryEscape(code))
	if gui2["errcode"].(float64) != 40163 {
		t.Fatalf("code 复用应 40163, got %v", gui2)
	}

	// 3. 缺 code / 非法 code
	gui3 := getJSON(srv.URL + "/cgi-bin/user/getuserinfo?access_token=" + tok)
	if gui3["errcode"].(float64) != 40029 {
		t.Fatalf("缺 code 应 40029, got %v", gui3)
	}

	// 4. appchat/send：自动建群并发消息
	body := `{"chatid":"wc_test","msgtype":"text","text":{"content":"hello"}}`
	r := postJSONMap(srv.URL+"/cgi-bin/appchat/send?access_token="+tok, body)
	if r["errcode"].(float64) != 0 {
		t.Fatalf("appchat/send 失败: %v", r)
	}
	chat, err := st.GetChatByChatid("wc_test")
	if err != nil {
		t.Fatalf("未自动建群: %v", err)
	}
	msgs, _ := st.ListMessages(chat.ID, 10)
	if len(msgs) == 0 || msgs[0].Content["content"] != "hello" {
		t.Fatalf("群聊消息异常: %+v", msgs)
	}

	// 5. appchat/send 缺 token / 空内容
	r = postJSONMap(srv.URL+"/cgi-bin/appchat/send", body)
	if r["errcode"].(float64) != 41001 {
		t.Fatalf("缺 token 应 41001, got %v", r)
	}
	r = postJSONMap(srv.URL+"/cgi-bin/appchat/send?access_token="+tok,
		`{"chatid":"wc_test","msgtype":"text","text":{"content":""}}`)
	if r["errcode"].(float64) != 40008 {
		t.Fatalf("空内容应 40008, got %v", r)
	}
}

func postJSONMap(url, body string) map[string]any {
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return map[string]any{"errcode": -1, "errmsg": err.Error()}
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

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

func tokenFor(t *testing.T, base, corpid, secret string) string {
	t.Helper()
	v := getJSON(base + "/cgi-bin/gettoken?corpid=" + url.QueryEscape(corpid) + "&corpsecret=" + url.QueryEscape(secret))
	if tok, ok := v["access_token"].(string); ok {
		return tok
	}
	return ""
}
