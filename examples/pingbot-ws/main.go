// pingbot-ws 是一个最小"接入方"示例：按企微智能机器人【长连接模式】接入本平台（M3）。
// 接入方以 wss 主动连入平台，aibot_subscribe（bot_id + secret）鉴权后，
// 平台以 aibot_msg_callback / aibot_event_callback 帧推送，接入方以 aibot_respond_msg 帧回复。
// 与 URL 回调模式完全互斥可选：机器人有活跃长连接时平台优先走长连接推送。
//
// 运行：
//
//	go run ./examples/pingbot-ws -ws-url ws://127.0.0.1:7788/cgi-bin/aibot/ws \
//	  -bot-id <aibotid> -secret <长连接密钥>
//
// 其中 <aibotid> 与 <长连接密钥> 从平台控制台"机器人"列表复制（bot_id 即 aibotid，
// secret 即机器人详情中的长连接密钥）。开启 TLS 时把 ws:// 换成 wss://。
package main

import (
	"flag"
	"log"

	"github.com/gorilla/websocket"
)

func main() {
	wsURL := flag.String("ws-url", "ws://127.0.0.1:7788/cgi-bin/aibot/ws", "平台长连接端点")
	botID := flag.String("bot-id", "", "机器人 aibotid（控制台复制）")
	secret := flag.String("secret", "", "机器人长连接密钥（控制台复制）")
	flag.Parse()
	if *botID == "" || *secret == "" {
		log.Fatal("必须提供 -bot-id 与 -secret（从平台控制台复制）")
	}

	c, _, err := websocket.DefaultDialer.Dial(*wsURL, nil)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer c.Close()

	// 订阅
	if err := c.WriteJSON(map[string]any{
		"type": "aibot_subscribe", "bot_id": *botID, "secret": *secret}); err != nil {
		log.Fatalf("订阅失败: %v", err)
	}
	var resp map[string]any
	if err := c.ReadJSON(&resp); err != nil {
		log.Fatalf("读取订阅应答失败: %v", err)
	}
	if code, _ := resp["code"].(float64); code != 0 {
		log.Fatalf("订阅被拒绝: %v", resp)
	}
	log.Printf("订阅成功（bot_id=%s）", *botID)

	for {
		var frame map[string]any
		if err := c.ReadJSON(&frame); err != nil {
			log.Printf("连接断开: %v", err)
			return
		}
		typ, _ := frame["type"].(string)
		switch typ {
		case "aibot_msg_callback":
			log.Printf("收到消息: msgid=%v chattype=%v msgtype=%v from=%v",
				frame["msgid"], frame["chattype"], frame["msgtype"], frame["from"])
			// 被动回复：一次性 stream（finish=true），以原 msgid 关联
			reply := map[string]any{
				"type":    "aibot_respond_msg",
				"msgid":   frame["msgid"],
				"msgtype": "stream",
				"stream":  map[string]any{"id": "pingbot-ws", "finish": true, "content": "**pingbot-ws** 收到：" + textOf(frame)},
			}
			if err := c.WriteJSON(reply); err != nil {
				log.Printf("回复失败: %v", err)
			}
		case "aibot_event_callback":
			log.Printf("收到事件: event=%v from=%v", frame["event"], frame["from"])
			// 事件回调同样可被动回复（如 enter_agent 欢迎语）
			reply := map[string]any{
				"type":    "aibot_respond_msg",
				"msgid":   frame["msgid"],
				"msgtype": "stream",
				"stream":  map[string]any{"id": "pingbot-ws", "finish": true, "content": "欢迎，我是 pingbot-ws"},
			}
			if err := c.WriteJSON(reply); err != nil {
				log.Printf("回复失败: %v", err)
			}
		default:
			log.Printf("未处理帧: %s", typ)
		}
	}
}

// textOf 从回调帧中取文本内容（text/markdown 均含 content 字段）。
func textOf(frame map[string]any) string {
	for _, k := range []string{"text", "markdown"} {
		if v, ok := frame[k].(map[string]any); ok {
			if content, ok := v["content"].(string); ok {
				return content
			}
		}
	}
	return ""
}
