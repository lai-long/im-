package store

import (
	"database/sql"
	"errors"
	"time"
)

// ErrNotFound 表示记录不存在。
var ErrNotFound = errors.New("store: not found")

// Corp / Bot / User / Chat 是核心实体。
type Corp struct {
	ID     int64
	CorpID string
	Name   string
}

type Bot struct {
	ID             int64  `json:"id"`
	CorpID         int64  `json:"corp_id"`
	Aibotid        string `json:"aibotid"`
	Name           string `json:"name"`
	CallbackURL    string `json:"callback_url"`
	CallbackToken  string `json:"callback_token"`
	CallbackAESKey string `json:"callback_aes_key"`
	CallbackMode   string `json:"callback_mode"` // plain | encrypted
	CallbackVerif  bool   `json:"callback_verified"`
}

type User struct {
	ID     int64  `json:"id"`
	CorpID int64  `json:"corp_id"`
	Userid string `json:"userid"`
	Name   string `json:"name"`
}

type Chat struct {
	ID     int64  `json:"id"`
	Chatid string `json:"chatid"`
	Name   string `json:"name"`
}

func scanBot(row interface{ Scan(...any) error }) (Bot, error) {
	var b Bot
	var verif int
	err := row.Scan(&b.ID, &b.CorpID, &b.Aibotid, &b.Name, &b.CallbackURL,
		&b.CallbackToken, &b.CallbackAESKey, &b.CallbackMode, &verif)
	b.CallbackVerif = verif == 1
	return b, err
}

const botCols = `id, corp_id, aibotid, name, callback_url, callback_token, callback_aes_key, callback_mode, callback_verified`

// GetBot 按 id 取机器人。
func (s *Store) GetBot(id int64) (Bot, error) {
	b, err := scanBot(s.db.QueryRow(`SELECT `+botCols+` FROM bot WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// GetBotByAibotid 按企微 aibotid 取机器人。
func (s *Store) GetBotByAibotid(aibotid string) (Bot, error) {
	b, err := scanBot(s.db.QueryRow(`SELECT `+botCols+` FROM bot WHERE aibotid=?`, aibotid))
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// UpdateBotCallback 保存机器人回调配置并重置验证状态。
func (s *Store) UpdateBotCallback(id int64, url, token, aesKey, mode string) error {
	_, err := s.db.Exec(`UPDATE bot SET callback_url=?, callback_token=?, callback_aes_key=?,
		callback_mode=?, callback_verified=0 WHERE id=?`, url, token, aesKey, mode, id)
	return err
}

// GetChatByWebhookKey 由 webhook key 定位 (群, 机器人) —— webhook/send 的路由入口。
func (s *Store) GetChatByWebhookKey(key string) (Chat, Bot, error) {
	var chat Chat
	var bot Bot
	err := s.db.QueryRow(`
		SELECT c.id, c.chatid, c.name,
		       b.id, b.corp_id, b.aibotid, b.name, b.callback_url,
		       b.callback_token, b.callback_aes_key, b.callback_mode, b.callback_verified
		FROM chat_bot cb
		JOIN chat c ON c.id = cb.chat_id
		JOIN bot  b ON b.id = cb.bot_id
		WHERE cb.webhook_key = ?`, key).Scan(
		&chat.ID, &chat.Chatid, &chat.Name,
		&bot.ID, &bot.CorpID, &bot.Aibotid, &bot.Name, &bot.CallbackURL,
		&bot.CallbackToken, &bot.CallbackAESKey, &bot.CallbackMode, new(int))
	if errors.Is(err, sql.ErrNoRows) {
		return chat, bot, ErrNotFound
	}
	return chat, bot, err
}

// CreateBot 创建机器人，生成 aibotid 与回调三元组。
func (s *Store) CreateBot(corpID int64, name string) (Bot, error) {
	res, err := s.db.Exec(`INSERT INTO bot(corp_id, aibotid, name, callback_token, callback_aes_key, created_at)
		VALUES(?,?,?,?,?,?)`, corpID, NewAibotid(), name, NewToken(), NewEncodingAESKey(), now())
	if err != nil {
		return Bot{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetBot(id)
}

// ListBots 列出全部机器人。
func (s *Store) ListBots() ([]Bot, error) {
	rows, err := s.db.Query(`SELECT ` + botCols + ` FROM bot ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bot
	for rows.Next() {
		b, err := scanBot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// AddBotToChat 把机器人拉进群并生成该群的 webhook key（key 按群分配）。
// 已在同一群时返回既有 key。
func (s *Store) AddBotToChat(chatID, botID int64) (string, error) {
	var key string
	err := s.db.QueryRow(`SELECT webhook_key FROM chat_bot WHERE chat_id=? AND bot_id=?`, chatID, botID).Scan(&key)
	if err == nil {
		return key, nil
	}
	key = NewUUID()
	if _, err := s.db.Exec(`INSERT INTO chat_bot(chat_id, bot_id, webhook_key) VALUES(?,?,?)`, chatID, botID, key); err != nil {
		return "", err
	}
	return key, nil
}

// ChatBotKey 是机器人在某群的 webhook key 条目（控制台展示）。
type ChatBotKey struct {
	ChatID int64  `json:"chat_id"`
	Name   string `json:"name"`
	Key    string `json:"webhook_key"`
}

// ChatBotKeys 列出机器人所在群及其 webhook key。
func (s *Store) ChatBotKeys(botID int64) ([]ChatBotKey, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.name, cb.webhook_key FROM chat_bot cb
		JOIN chat c ON c.id = cb.chat_id WHERE cb.bot_id = ? ORDER BY c.id`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatBotKey
	for rows.Next() {
		var r ChatBotKey
		if err := rows.Scan(&r.ChatID, &r.Name, &r.Key); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// ResetCallbackTask 把任务复位为 pending 以便重放（控制台手动重推）。
func (s *Store) ResetCallbackTask(id int64) error {
	_, err := s.db.Exec(`UPDATE callback_task SET status='pending', attempt=0, next_retry_at=0,
		last_error='', updated_at=? WHERE id=?`, now(), id)
	return err
}

// GetChat 取群。
func (s *Store) GetChat(id int64) (Chat, error) {
	var c Chat
	err := s.db.QueryRow(`SELECT id, chatid, name FROM chat WHERE id=?`, id).
		Scan(&c.ID, &c.Chatid, &c.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// GetUserByID 按内部 id 取用户。
func (s *Store) GetUserByID(id int64) (User, error) {
	var u User
	err := s.db.QueryRow(`SELECT id, corp_id, userid, name FROM "user" WHERE id=?`, id).
		Scan(&u.ID, &u.CorpID, &u.Userid, &u.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// GetUserByUserid 按企微 userid 取用户。
func (s *Store) GetUserByUserid(userid string) (User, error) {
	var u User
	err := s.db.QueryRow(`SELECT id, corp_id, userid, name FROM "user" WHERE userid=?`, userid).
		Scan(&u.ID, &u.CorpID, &u.Userid, &u.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// SeedWebhookInfo 返回开箱即用接入信息：默认群 webhook key 与示例机器人回调三元组。
// 启动日志据此打印（方案文档 §6.1）。
type SeedWebhookInfo struct {
	WebhookKey     string
	BotName        string
	CallbackToken  string
	CallbackAESKey string
}

func (s *Store) SeedWebhookInfo() (SeedWebhookInfo, error) {
	var info SeedWebhookInfo
	info.WebhookKey = SeedWebhookKey
	err := s.db.QueryRow(`
		SELECT b.name, b.callback_token, b.callback_aes_key
		FROM chat_bot cb JOIN bot b ON b.id = cb.bot_id
		WHERE cb.webhook_key = ?`, SeedWebhookKey).
		Scan(&info.BotName, &info.CallbackToken, &info.CallbackAESKey)
	if errors.Is(err, sql.ErrNoRows) {
		return info, ErrNotFound
	}
	return info, err
}

func now() int64 { return time.Now().Unix() }

// ListChats 列出全部群。
func (s *Store) ListChats() ([]Chat, error) {
	rows, err := s.db.Query(`SELECT id, chatid, name FROM chat ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var c Chat
		if err := rows.Scan(&c.ID, &c.Chatid, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// FirstCorp 返回预置默认企业。
func (s *Store) FirstCorp() (Corp, error) {
	var c Corp
	err := s.db.QueryRow(`SELECT id, corpid, name FROM corp ORDER BY id LIMIT 1`).Scan(&c.ID, &c.CorpID, &c.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// MarkCallbackVerified 标记回调配置已通过 URL 验证。
func (s *Store) MarkCallbackVerified(botID int64) error {
	_, err := s.db.Exec(`UPDATE bot SET callback_verified=1 WHERE id=?`, botID)
	return err
}
