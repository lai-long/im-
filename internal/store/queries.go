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
	ID             int64
	CorpID         int64
	Aibotid        string
	Name           string
	CallbackURL    string
	CallbackToken  string
	CallbackAESKey string
	CallbackMode   string // plain | encrypted
	CallbackVerif  bool
}

type User struct {
	ID     int64
	CorpID int64
	Userid string
	Name   string
}

type Chat struct {
	ID     int64
	Chatid string
	Name   string
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
