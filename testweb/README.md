# TestWeb IM

一个简易 IM 系统，用于平台联调测试。前端为 React + TypeScript（Vite 构建），
后端为 Go（HTTP + WebSocket），数据全部保存在内存中，重启即清空。

testweb 扮演「IM 平台」的角色：浏览器页面是 IM 客户端，对外暴露
webhook 回调和主动发消息接口，供 Agent 平台（trpc-service）像对接
真实 IM（企微/微信客服）一样对接它。

## 启动

一键启动（推荐，在项目根目录执行，自动装依赖、构建前后端并后台运行）：

```bash
make testweb        # 启动，默认 :8080，日志在 data/testweb.log
make testweb-stop   # 停止
make testweb-run    # 前台运行（调试）
make testweb-dev    # 前端开发模式（Vite 热更新，:5173 代理到 :8080）
```

也可以换端口：`make testweb ADDR=:9090`。

手动方式：
cd cmd/testweb/web
npm install
npm run build        # 产出 web/dist，由 Go 内嵌

cd ../../..
go build -o bin/testweb ./cmd/testweb
./bin/testweb -addr :8080
```

打开 http://localhost:8080，输入昵称进入群聊。可多开浏览器标签模拟多用户。

前端开发模式（热更新，代理到 Go 后端）：

```bash
cd cmd/testweb/web && npm run dev   # http://localhost:5173
```

## 接口

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/messages?limit=N` | 历史消息（升序），limit<=0 为全部（最多保留 500 条） |
| POST | `/api/send` | 主动发消息接口，body `{"user":"u","text":"hello"}`，模拟 IM 平台的 message/send |
| GET | `/api/users` | 当前在线用户（WS 连接数） |
| GET/POST/DELETE | `/api/webhooks` | 回调订阅管理：POST `{"url":"..."}` 注册，DELETE `?url=` 注销，GET 列表 |
| WS | `/ws?user=xxx` | IM 客户端实时通道（仅 testweb 页面内部使用） |

## Webhook 回调

注册回调地址后，任何消息（聊天 / 加入 / 离开）都会异步 POST 到该地址：

```json
{"msg_id": 3, "user": "bob", "text": "hi", "kind": "chat", "ts": 1787649377413}
```

- 订阅方返回 2xx 视为应答成功；否则 testweb 会重试最多 3 次（间隔 2s/4s），
  模拟真实 IM 的重推，订阅方应按 `msg_id` 幂等去重。
- 这对应设计方案中「Gateway 验签去重 → 立即应答 success」的链路；
  Agent 平台的回复可走 `POST /api/send` 回到 IM。
