# 接入方示例（pingbot）

两个最小"接入方"实现，演示如何按**企微智能机器人 URL 回调模式**接入本平台。
两者都不复用平台内部代码：Go 版使用企微官方加解密库，Python 版按公开规范独立实现
（用于验证跨语言互通）。

## 使用步骤

1. 启动平台：`make run`，从启动日志（或控制台）拿到示例机器人的
   `Token` 与 `EncodingAESKey`。
2. 启动示例接入方：
   - Go：`go run ./examples/pingbot-go -addr :9000 -token <Token> -aeskey <AESKey>`
   - Python：`pip install cryptography`，然后
     `python3 examples/pingbot-python/pingbot.py --token <Token> --aeskey <AESKey>`
3. 平台控制台 → 机器人 → 回调 URL 填 `http://127.0.0.1:9000/wecom` → 保存并验证。
   保存动作即触发与企微相同的 echostr 握手（本地无内网穿透要求）。
4. 在群聊页以任意用户发送 `@示例机器人 你好`，接入方会收到加密回调，
   并以一次性 `stream`（`finish=true`）被动回复，回复落在群里。

## 主动发消息（webhook/send）

```bash
curl -X POST 'http://127.0.0.1:7788/cgi-bin/webhook/send?key=<webhook_key>' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"hello"}}'
```

## 切换真实企微

本示例代码无需改动，只需把目标地址从 `http://127.0.0.1:7788` 换成
`https://qyapi.weixin.qq.com`，并在企微管理后台配置相同的回调三元组。
