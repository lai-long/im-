// pingbot-go 是一个最小"接入方"示例：按企微智能机器人 URL 回调模式实现接收侧，
// 用企微官方加解密库（sbzhu/weworkapi_golang）验签/解密，并以一次性 stream 被动回复。
//
// 用途：
//  1. 演示"接入方"应如何接入本平台（与真实企微完全相同的代码）；
//  2. 作为跨实现互通的验证样本（不复用平台内部实现）。
//
// 运行：
//
//	go run ./examples/pingbot-go -addr :9000 -token <Token> -aeskey <EncodingAESKey>
//
// 然后在平台控制台把回调 URL 填为 http://127.0.0.1:9000/wecom，Token/AESKey 与此处一致，
// 保存时会触发 URL 验证握手；验证通过后，在群里 @机器人 即可收到回调。
package main

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"log"
	"net/http"

	official "github.com/sbzhu/weworkapi_golang/wxbizmsgcrypt"
)

func main() {
	addr := flag.String("addr", ":9000", "监听地址")
	path := flag.String("path", "/wecom", "回调路径")
	token := flag.String("token", "", "回调 Token（平台控制台生成）")
	aesKey := flag.String("aeskey", "", "回调 EncodingAESKey（平台控制台生成）")
	flag.Parse()
	if *token == "" || *aesKey == "" {
		log.Fatal("必须提供 -token 与 -aeskey（从平台控制台复制）")
	}

	// 智能机器人场景 receiveid 为空字符串（与自建应用的 corpid 不同）
	cpt := official.NewWXBizMsgCrypt(*token, *aesKey, "", official.XmlType)

	http.HandleFunc(*path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.Method {
		case http.MethodGet:
			// URL 验证：解密 echostr 并原样返回明文（无引号、无 BOM、无换行）
			plain, cerr := cpt.VerifyURL(q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce"), q.Get("echostr"))
			if cerr != nil {
				log.Printf("验证失败: %+v", cerr)
				http.Error(w, "verify failed", http.StatusBadRequest)
				return
			}
			log.Printf("URL 验证通过")
			_, _ = w.Write(plain)

		case http.MethodPost:
			// 平台推送的信封为 JSON {"encrypt": "..."}，官方库期望 XML，这里做个转换
			var env struct {
				Encrypt string `json:"encrypt"`
			}
			if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
				http.Error(w, "bad envelope", http.StatusBadRequest)
				return
			}
			msg, cerr := cpt.DecryptMsg(q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce"),
				[]byte("<xml><Encrypt><![CDATA["+env.Encrypt+"]]></Encrypt></xml>"))
			if cerr != nil {
				log.Printf("解密失败: %+v", cerr)
				http.Error(w, "decrypt failed", http.StatusBadRequest)
				return
			}
			log.Printf("收到回调: %s", truncate(string(msg), 400))

			var p struct {
				Msgid    string `json:"msgid"`
				Chatid   string `json:"chatid"`
				Chattype string `json:"chattype"`
				From     struct {
					Userid string `json:"userid"`
				} `json:"from"`
				ResponseURL string `json:"response_url"`
				MsgType     string `json:"msgtype"`
				Text        struct {
					Content string `json:"content"`
				} `json:"text"`
			}
			_ = json.Unmarshal(msg, &p)

			// 被动回复：一次性 stream（finish=true）；完整内容直接返回
			reply := map[string]any{
				"msgtype": "stream",
				"stream": map[string]any{
					"id":      p.Msgid,
					"finish":  true,
					"content": "**pingbot** 收到：" + p.Text.Content,
				},
			}
			raw, cerr := cpt.EncryptMsg(mustJSON(reply), q.Get("timestamp"), q.Get("nonce"))
			if cerr != nil {
				http.Error(w, "encrypt failed", http.StatusBadRequest)
				return
			}
			var out struct {
				Encrypt string `xml:"Encrypt"`
			}
			_ = xml.Unmarshal(raw, &out)
			_ = json.NewEncoder(w).Encode(map[string]string{"encrypt": out.Encrypt})

			// 也可改用 response_url 异步主动回复（一次性、1 小时有效）：
			//   http.Post(p.ResponseURL, "application/json",
			//       strings.NewReader(`{"msgtype":"markdown","markdown":{"content":"异步回复"}}`))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// 手动发消息到群（演示 webhook/send）：
	//   curl -X POST 'http://<平台>/cgi-bin/webhook/send?key=<key>' \
	//     -d '{"msgtype":"text","text":{"content":"hello"}}'
	log.Printf("pingbot-go 监听 %s%s（token=%s）", *addr, *path, *token)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal(err)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
