# 本地 IM 平台（企业微信兼容）

本地运行的 IM 平台，对外提供与**企业微信智能机器人/群机器人**完全一致的接入方式。
接入方只需把 API 域名从 `https://qyapi.weixin.qq.com` 换成本机地址，即可完成本地联调——
**不需要内网穿透**（企微云端不接受内网回调 URL，本平台没有此限制）。

- 设计文档：[docs/设计文档.md](docs/设计文档.md)
- 方案文档：[docs/方案文档.md](docs/方案文档.md)
- 错误码对照表：[docs/错误码对照表.md](docs/错误码对照表.md)
- 接入方示例：[examples/README.md](examples/README.md)

## 快速开始

```bash
make run          # 默认 :7788，首次启动自动初始化数据目录 ./data
```

启动后日志会打印开箱即用的接入信息：

```
控制台:   http://127.0.0.1:7788/admin
IM 客户端: http://127.0.0.1:7788/
示例机器人 示例机器人
  webhook: http://127.0.0.1:7788/cgi-bin/webhook/send?key=693a91f6-...
  回调配置: Token=... EncodingAESKey=...（在控制台保存回调 URL 后生效）
```

预置数据：默认企业、默认群、用户 `zhangsan`（张三）/ `lisi`（李四）、
已在群内的示例机器人（webhook key 固定可预测）。

局域网协作用 `-public-url`（保证回调里的 response_url 对外可达）：

```bash
./im-server -addr :7788 -public-url http://192.168.1.10:7788 -data ./data
```

## 5 分钟冒烟

1. **机器人发消息到群**（控制台/日志里取 key）：

```bash
curl -X POST 'http://127.0.0.1:7788/cgi-bin/webhook/send?key=<webhook_key>' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"hello","mentioned_list":["zhangsan"]}}'
# {"errcode":0,"errmsg":"ok"}  → 打开 http://127.0.0.1:7788/ 可见机器人消息
```

2. **接入方接收回调**：用示例接入方（见 [examples/README.md](examples/README.md)），
   或自己的服务实现 URL 验证 + 解密；在控制台把回调 URL 填成你的地址，保存即触发
   echostr 握手。

3. **群里 @机器人 → 接入方收到加密回调 → 回复落群**：
   在群聊页发送 `@示例机器人 你好`，接入方以一次性 `stream`（`finish=true`）被动回复，
   回复出现在群里；也可用回调中的 `response_url` 异步主动回复（一次性、1 小时有效）。

4. **一键自测**（无需任何外部依赖，内置 mock 接入方跑完验收全链路，13 项）：

```bash
make selftest
```

   覆盖：URL 验证握手、webhook/send（含错误码）、@机器人回调、流式多轮刷新、
   response_url（一次性语义）、template_card、TLS 自签证书。

5. **流式回复**：接入方被动回复 `finish=false` 的 stream 消息，平台按 1s 节奏轮询刷新，
   客户端同一条气泡实时更新，直至 `finish=true` 或 6 分钟窗口结束。

## 当前阶段

- **M1a 已完成**：webhook/send（text/markdown）、回调分发（加密推送、5s 超时重推 3 次、死信、
  被动回复）、response_url 主动回复、WS 实时群聊、控制台（机器人/群管理、回调保存即验证、
  消息流水、回调重放）、回环自测、Go/Python 接入方示例。
- **M1b 已完成**：流式回复轮询（1s 节奏、全量刷新覆盖同一条消息、6 分钟窗口）、
  template_card（被动回复与 response_url）、TLS 自签证书（`-tls`）。
- **M2 起**：自建应用兼容（gettoken / message/send / XML 回调）、素材（webhook/upload_media）、
  机器人单聊、明文回调模式。
- **M3**：智能机器人长连接模式同形模拟、卡片交互事件、OAuth2、hosts 劫持脚本。

## 目录结构

```
cmd/im-server/      入口
internal/api/       企微兼容层（/cgi-bin/*）与客户端接口（/api/*）
internal/callback/  回调分发器：加解密、URL 验证、推送队列与重试
internal/core/      消息核心：落库、@解析、事件广播
internal/admin/     管理控制台 API
internal/ws/        WebSocket 网关
internal/store/     SQLite 访问层（迁移 + CRUD）
internal/selftest/  回环自测
internal/server/    组装 + 嵌入式 Web 页面
examples/           接入方示例（Go / Python）
docs/               设计、方案、错误码对照表
testweb/            早期验证用 demo，M1 稳定后将移除
```

## 常用命令

```bash
make build      # 构建 im-server
make run        # 启动
make test       # 单元测试（含与企微官方加解密库的互通用例）
make selftest   # 回环自测：跑完 M1 验收全链路
make clean
```
