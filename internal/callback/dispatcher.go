package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"im-/internal/core"
	"im-/internal/store"
)

// 重试对齐企微：最多重推 3 次，间隔 2s/4s/8s（共 4 次推送）。
var defaultBackoff = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

const pushTimeout = 5 * time.Second

// 流式回复：官方未公开轮询节奏，平台取固定值（可配置）；最长窗口 6 分钟（设计文档 §4.3）。
const (
	defaultStreamTick   = 1 * time.Second
	defaultStreamWindow = 6 * time.Minute
)

// Dispatcher 把用户 @机器人 的消息按智能机器人格式推给接入方，
// 并消费接入方的被动回复落群（含流式全量刷新）。
type Dispatcher struct {
	st           *store.Store
	base         string // 平台对外 Base URL（response_url 用）
	core         *core.Service
	cli          *http.Client
	wake         chan struct{}
	stop         chan struct{}
	Backoff      []time.Duration // 重试间隔（默认对齐企微 2s/4s/8s；测试可注入）
	StreamTick   time.Duration   // 流式刷新轮询节奏（默认 1s）
	StreamWindow time.Duration   // 流式最长窗口（默认 6 分钟，对齐企微）
}

// NewDispatcher 创建分发器。
func NewDispatcher(st *store.Store, baseURL string, coreSvc *core.Service) *Dispatcher {
	return &Dispatcher{
		st:           st,
		base:         strings.TrimRight(baseURL, "/"),
		core:         coreSvc,
		cli:          &http.Client{Timeout: pushTimeout},
		wake:         make(chan struct{}, 1),
		stop:         make(chan struct{}),
		Backoff:      defaultBackoff,
		StreamTick:   defaultStreamTick,
		StreamWindow: defaultStreamWindow,
	}
}

// Start 启动恢复与消费循环（M1a 全局串行队列：保序；单机本地工具足够）。
func (d *Dispatcher) Start() {
	if err := d.st.RecoverProcessing(); err != nil {
		fmt.Printf("恢复 pending 回调任务失败: %v\n", err)
	}
	go d.loop()
}

func (d *Dispatcher) loop() {
	for {
		select {
		case <-d.stop:
			return
		case <-d.wake:
		default:
		}
		task, err := d.st.NextPendingTask()
		if errors.Is(err, store.ErrNotFound) {
			select {
			case <-d.stop:
				return
			case <-time.After(300 * time.Millisecond):
			case <-d.wake:
			}
			continue
		} else if err != nil {
			time.Sleep(time.Second)
			continue
		}
		d.process(task)
	}
}

// EnqueueUserMessage 在用户 @机器人 时创建推送任务。
func (d *Dispatcher) EnqueueUserMessage(msg store.Message, bot store.Bot) {
	chat, err := d.st.GetChat(msg.ChatID)
	if err != nil {
		return
	}
	user, err := d.st.GetUserByID(msg.SenderID)
	if err != nil {
		return
	}
	responseCode := store.NewRandomString(24)
	payload := map[string]any{
		"msgid":    msg.Msgid,
		"aibotid":  bot.Aibotid,
		"chatid":   chat.Chatid,
		"chattype": "group",
		"from":     map[string]any{"userid": user.Userid},
		"msgtype":  msg.MsgType,
		"msg":      msg.Content,
		"response_url": fmt.Sprintf("%s/cgi-bin/aibot/response?response_code=%s",
			d.base, responseCode),
	}
	switch msg.MsgType {
	case "text":
		payload["text"] = msg.Content
	case "markdown":
		payload["markdown"] = msg.Content
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if _, err := d.st.CreateCallbackTask(msg.ID, bot.ID, string(raw), responseCode,
		time.Now().Add(store.ResponseCodeTTL).Unix()); err != nil {
		return
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// process 推送单条任务并处理被动回复。
func (d *Dispatcher) process(task store.CallbackTask) {
	if err := d.st.MarkTaskProcessing(task.ID); err != nil {
		return
	}
	bot, err := d.st.GetBot(task.BotID)
	if err != nil {
		_ = d.st.FinishTask(task.ID, "bot missing")
		return
	}
	msg, err := d.st.MessageByID(task.MessageID)
	if err != nil {
		_ = d.st.FinishTask(task.ID, "message missing")
		return
	}

	if bot.CallbackURL == "" {
		_ = d.st.FinishTask(task.ID, "callback URL 未配置，未推送")
		return
	}

	if task.Status == "streaming" {
		d.processStream(task, bot, msg)
		return
	}

	attempt := task.Attempt + 1
	lastErr, reply := d.push(bot, task.Payload)
	if lastErr == "" {
		// 无回复（success/空）→ 完成；有回复 → 按类型落群，流式则进入轮询
		note := ""
		switch {
		case reply == nil:
			// 无回复
		case reply.MsgType == "stream":
			if reply.Stream == nil || reply.Stream.Content == "" {
				lastErr = "stream 回复缺少内容"
				break
			}
			streamID := reply.Stream.ID
			if streamID == "" {
				streamID = store.NewRandomString(8)
			}
			m := d.deliver(msg.ChatID, bot, "stream", map[string]any{
				"content": reply.Stream.Content, "finish": reply.Stream.Finish})
			if reply.Stream.Finish {
				note = "流式回复完成（一次性）"
			} else {
				_ = d.st.StartStream(task.ID, streamID, m.ID, time.Now().Add(d.StreamTick).Unix())
				d.wakeLoop()
				return
			}
		case reply.MsgType == "template_card":
			if len(reply.TemplateCard) == 0 {
				lastErr = "template_card 回复缺少内容"
				break
			}
			d.deliver(msg.ChatID, bot, "template_card", reply.TemplateCard)
			note = "template_card 已落群"
		case reply.MsgType == "text":
			// 官方语义：text 仅"进入会话欢迎语"场景支持（M2）
			lastErr = "text 被动回复仅欢迎语场景支持（M2），已拒绝"
		default:
			lastErr = "不支持的被动回复类型: " + reply.MsgType
		}
		if lastErr == "" {
			_ = d.st.FinishTask(task.ID, note)
			return
		}
	}
	if attempt <= len(d.Backoff) {
		_ = d.st.RetryTask(task.ID, attempt, time.Now().Add(d.Backoff[attempt-1]).Unix(), lastErr, false)
	} else {
		_ = d.st.RetryTask(task.ID, attempt, 0, lastErr, true)
	}
	d.wakeLoop()
}

// processStream 处理一轮流式刷新：推送 stream.id 回调，接入方回全量内容，
// 平台更新同一条消息；finish=true 或超过 6 分钟窗口则结束。
func (d *Dispatcher) processStream(task store.CallbackTask, bot store.Bot, msg store.Message) {
	// 窗口结束：自用户消息起 6 分钟
	if task.CreatedAt > 0 && time.Since(time.Unix(task.CreatedAt, 0)) > d.StreamWindow {
		_ = d.st.FinishTask(task.ID, "流式窗口结束（6 分钟）")
		return
	}
	chat, err := d.st.GetChat(msg.ChatID)
	if err != nil {
		_ = d.st.FinishTask(task.ID, "chat missing")
		return
	}
	user, err := d.st.GetUserByID(msg.SenderID)
	if err != nil {
		_ = d.st.FinishTask(task.ID, "user missing")
		return
	}
	payload, err := json.Marshal(map[string]any{
		"msgid":    msg.Msgid,
		"aibotid":  bot.Aibotid,
		"chatid":   chat.Chatid,
		"chattype": "group",
		"from":     map[string]any{"userid": user.Userid},
		"msgtype":  "stream",
		"stream":   map[string]any{"id": task.StreamID},
	})
	if err != nil {
		_ = d.st.FinishTask(task.ID, "流式载荷构造失败")
		return
	}

	attempt := task.Attempt + 1
	lastErr, reply := d.push(bot, string(payload))
	if lastErr != "" {
		if attempt <= len(d.Backoff) {
			_ = d.st.RetryTask(task.ID, attempt, time.Now().Add(d.Backoff[attempt-1]).Unix(), lastErr, false)
		} else {
			_ = d.st.RetryTask(task.ID, attempt, 0, lastErr, true)
		}
		d.wakeLoop()
		return
	}
	// 流式刷新应答必须是 stream
	if reply == nil || reply.MsgType != "stream" || reply.Stream == nil {
		_ = d.st.FinishTask(task.ID, "流式刷新应答非 stream，已结束")
		return
	}
	stream := reply.Stream
	if task.StreamMsgID != 0 {
		updated, err := d.st.UpdateMessageContent(task.StreamMsgID, map[string]any{
			"content": stream.Content,
			"finish":  stream.Finish,
		})
		if err == nil && d.core != nil {
			d.core.BroadcastStream(updated)
		}
	}
	if stream.Finish {
		_ = d.st.FinishTask(task.ID, "流式回复完成")
		return
	}
	_ = d.st.ContinueStream(task.ID, time.Now().Add(d.StreamTick).Unix())
	d.wakeLoop()
}

// wakeLoop 唤醒消费循环（不阻塞）。
func (d *Dispatcher) wakeLoop() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// replyPayload 是解析后的被动回复。
type replyPayload struct {
	MsgType string `json:"msgtype"`
	Stream  *struct {
		ID      string `json:"id"`
		Finish  bool   `json:"finish"`
		Content string `json:"content"`
	} `json:"stream"`
	TemplateCard map[string]any `json:"template_card"`
}

// push 执行一次加密推送，返回错误描述与解析后的被动回复（无回复时为 nil）。
func (d *Dispatcher) push(bot store.Bot, payload string) (string, *replyPayload) {
	var body []byte
	postURL := bot.CallbackURL
	ts := fmt.Sprintf("%d", time.Now().Unix())
	nonce := store.NewRandomString(8)

	switch bot.CallbackMode {
	case "plain":
		body = []byte(payload)
	default: // encrypted（默认）
		enc, err := Encrypt(payload, bot.CallbackAESKey, "")
		if err != nil {
			return "加密失败: " + err.Error(), nil
		}
		body, _ = json.Marshal(map[string]string{"encrypt": enc})
		sig := Signature(bot.CallbackToken, ts, nonce, enc)
		sep := "?"
		if strings.Contains(postURL, "?") {
			sep = "&"
		}
		q := url.Values{"msg_signature": {sig}, "timestamp": {ts}, "nonce": {nonce}}
		postURL += sep + q.Encode()
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, postURL, bytes.NewReader(body))
	if err != nil {
		return err.Error(), nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.cli.Do(req)
	if err != nil {
		return "推送失败: " + err.Error(), nil
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("接入方应答 %d: %s", resp.StatusCode, truncate(string(respBody), 200)), nil
	}
	return d.parseReply(bot, respBody)
}

// parseReply 解析被动回复：空/success 表示无回复；
// 密文模式下 body 为 {"encrypt":...} 需先解密，明文模式下 body 即回复原文。
func (d *Dispatcher) parseReply(bot store.Bot, body []byte) (string, *replyPayload) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "success" {
		return "", nil
	}
	var env struct {
		Encrypt string `json:"encrypt"`
	}
	if bot.CallbackMode == "plain" {
		// 明文模式：body 直接是回复 JSON（调试用，无签名无加密）
		if err := json.Unmarshal(body, &env); err == nil && env.Encrypt != "" {
			return "明文模式下被动回复不应为加密信封", nil
		}
		var reply replyPayload
		if err := json.Unmarshal(body, &reply); err != nil {
			return "被动回复 JSON 解析失败: " + err.Error(), nil
		}
		return "", &reply
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Encrypt == "" {
		return "被动回复格式非法（既非 success 也非 {\"encrypt\":...}）: " + truncate(trimmed, 200), nil
	}
	plain := trimmed
	if bot.CallbackMode != "plain" {
		dec, _, err := Decrypt(env.Encrypt, bot.CallbackAESKey, "")
		if err != nil {
			return "被动回复解密失败: " + err.Error(), nil
		}
		plain = dec
	}
	var reply replyPayload
	if err := json.Unmarshal([]byte(plain), &reply); err != nil {
		return "被动回复 JSON 解析失败: " + err.Error(), nil
	}
	return "", &reply
}

// deliver 把回复以机器人身份落入指定群，返回落库消息。
func (d *Dispatcher) deliver(chatID int64, bot store.Bot, msgType string, content map[string]any) store.Message {
	if d.core == nil || chatID == 0 {
		return store.Message{}
	}
	m, _ := d.core.BotMessage(chatID, bot.ID, msgType, content, nil)
	return m
}

// VerifyCallback 模拟企微"保存回调配置"时的 URL 验证握手：
// GET url?msg_signature&timestamp&nonce&echostr，接入方须在 1 秒内
// 返回解密后的明文 echostr（无引号、无 BOM、无换行）。
func (d *Dispatcher) VerifyCallback(rawURL, token, encodingAESKey string) error {
	echoPlain := "verify-" + store.NewRandomString(12)
	enc, err := Encrypt(echoPlain, encodingAESKey, "")
	if err != nil {
		return err
	}
	ts := fmt.Sprintf("%d", time.Now().Unix())
	nonce := store.NewRandomString(8)
	sig := Signature(token, ts, nonce, enc)
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	q := url.Values{"msg_signature": {sig}, "timestamp": {ts}, "nonce": {nonce}, "echostr": {enc}}
	vURL := rawURL + sep + q.Encode()

	cli := &http.Client{Timeout: 1 * time.Second} // 对齐企微 1 秒应答要求
	resp, err := cli.Get(vURL)
	if err != nil {
		return fmt.Errorf("验证请求失败: %w", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("验证应答 %d", resp.StatusCode)
	}
	if strings.TrimSpace(string(got)) != echoPlain {
		return errors.New("echostr 解密回显不匹配")
	}
	return nil
}

// Stop 停止消费循环。
func (d *Dispatcher) Stop() { close(d.stop) }

// truncate 截断长文本（错误信息展示用）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
