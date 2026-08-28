package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// CallbackTask 是一次待推送的回调任务。
type CallbackTask struct {
	ID           int64  `json:"id"`
	MessageID    int64  `json:"message_id"`
	BotID        int64  `json:"bot_id"`
	Payload      string `json:"payload"` // 加密前明文 JSON
	ResponseCode string `json:"response_code"`
	StreamID     string `json:"stream_id"`
	StreamFin    bool   `json:"stream_finished"`
	Status       string `json:"status"`
	Attempt      int    `json:"attempt"`
	NextRetry    int64  `json:"next_retry_at"`
	LastError    string `json:"last_error"`
}

// ResponseCodeTTL 是 response_url 的有效期（企微：1 小时）。
const ResponseCodeTTL = time.Hour

// CreateCallbackTask 为一条用户消息对某机器人创建推送任务。
// payload 需已包含 response_url；responseCode/过期时间由调用方生成后传入。
func (s *Store) CreateCallbackTask(messageID, botID int64, payload, responseCode string, expireAt int64) (CallbackTask, error) {
	t := CallbackTask{MessageID: messageID, BotID: botID, Payload: payload,
		ResponseCode: responseCode, Status: "pending"}
	res, err := s.db.Exec(`INSERT INTO callback_task
		(message_id, bot_id, payload, response_code, response_expire_at, status, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		messageID, botID, payload, responseCode, expireAt, "pending", now(), now())
	if err != nil {
		return t, err
	}
	t.ID, _ = res.LastInsertId()
	return t, nil
}

// NextPendingTask 取一条到期的待推送任务（全局串行，按 id 保序）。
func (s *Store) NextPendingTask() (CallbackTask, error) {
	var t CallbackTask
	var fin int
	err := s.db.QueryRow(`SELECT id, message_id, bot_id, payload, response_code,
		stream_id, stream_finished, status, attempt, next_retry_at, last_error
		FROM callback_task WHERE status='pending' AND next_retry_at <= ?
		ORDER BY id LIMIT 1`, now()).
		Scan(&t.ID, &t.MessageID, &t.BotID, &t.Payload, &t.ResponseCode,
			&t.StreamID, &fin, &t.Status, &t.Attempt, &t.NextRetry, &t.LastError)
	t.StreamFin = fin == 1
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// MarkTaskProcessing 置为处理中。
func (s *Store) MarkTaskProcessing(id int64) error {
	_, err := s.db.Exec(`UPDATE callback_task SET status='processing', attempt=attempt+1, updated_at=? WHERE id=?`, now(), id)
	return err
}

// FinishTask 成功完成。
func (s *Store) FinishTask(id int64, lastErr string) error {
	_, err := s.db.Exec(`UPDATE callback_task SET status='done', last_error=?, stream_finished=1, updated_at=? WHERE id=?`,
		lastErr, now(), id)
	return err
}

// RetryTask 记录失败并安排重试；超过 maxAttempts 置死信。
func (s *Store) RetryTask(id int64, attempt int, nextRetryAt int64, lastErr string, dead bool) error {
	status := "pending"
	if dead {
		status = "dead"
	}
	_, err := s.db.Exec(`UPDATE callback_task SET status=?, attempt=?, next_retry_at=?, last_error=?, updated_at=? WHERE id=?`,
		status, attempt, nextRetryAt, lastErr, now(), id)
	return err
}

// RecoverProcessing 启动时把遗留 processing 复位为 pending。
func (s *Store) RecoverProcessing() error {
	_, err := s.db.Exec(`UPDATE callback_task SET status='pending', updated_at=? WHERE status='processing'`, now())
	return err
}

// MessageByID 按内部 id 取消息。
func (s *Store) MessageByID(id int64) (Message, error) {
	var m Message
	var cj, mstr string
	err := s.db.QueryRow(`
		SELECT m.id, m.msgid, m.chat_id, m.sender_type, m.sender_id, m.msg_type, m.content_json, m.mentioned, m.created_at
		FROM message m WHERE m.id=?`, id).
		Scan(&m.ID, &m.Msgid, &m.ChatID, &m.SenderTyp, &m.SenderID, &m.MsgType, &cj, &mstr, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal([]byte(cj), &m.Content); err != nil {
		return m, err
	}
	return m, nil
}

// ListCallbackTasks 按状态列出回调任务（控制台观测）。
func (s *Store) ListCallbackTasks(status string, limit int) ([]CallbackTask, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, message_id, bot_id, payload, response_code, stream_id, stream_finished,
		status, attempt, next_retry_at, last_error FROM callback_task`
	var args []any
	if status != "" {
		q += ` WHERE status=?`
		args = append(args, status)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CallbackTask
	for rows.Next() {
		var t CallbackTask
		var fin int
		if err := rows.Scan(&t.ID, &t.MessageID, &t.BotID, &t.Payload, &t.ResponseCode,
			&t.StreamID, &fin, &t.Status, &t.Attempt, &t.NextRetry, &t.LastError); err != nil {
			return nil, err
		}
		t.StreamFin = fin == 1
		out = append(out, t)
	}
	return out, nil
}

// ValidResponseTask 校验 response_code：未用、未过期；返回任务。
func (s *Store) ValidResponseTask(code string) (CallbackTask, error) {
	var t CallbackTask
	var fin int
	err := s.db.QueryRow(`SELECT id, message_id, bot_id, payload, response_code,
		stream_id, stream_finished, status, attempt, next_retry_at, last_error, response_used, response_expire_at
		FROM callback_task WHERE response_code=?`, code).
		Scan(&t.ID, &t.MessageID, &t.BotID, &t.Payload, &t.ResponseCode,
			&t.StreamID, &fin, &t.Status, &t.Attempt, &t.NextRetry, &t.LastError,
			new(int), new(int64))
	t.StreamFin = fin == 1
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	var used int
	var exp int64
	if err := s.db.QueryRow(`SELECT response_used, response_expire_at FROM callback_task WHERE response_code=?`, code).
		Scan(&used, &exp); err != nil {
		return t, err
	}
	if used == 1 {
		return t, errors.New("response_code already used")
	}
	if exp < now() {
		return t, errors.New("response_code expired")
	}
	return t, nil
}

// ConsumeResponseTask 标记 response_code 已使用（一次性语义）。
// 乐观并发：仅当仍未使用时更新成功。
func (s *Store) ConsumeResponseTask(code string) (bool, error) {
	res, err := s.db.Exec(`UPDATE callback_task SET response_used=1
		WHERE response_code=? AND response_used=0 AND response_expire_at >= ?`, code, now())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
