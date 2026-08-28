package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Message 是一条群消息。
type Message struct {
	ID        int64          `json:"id"`
	Msgid     string         `json:"msgid"`
	ChatID    int64          `json:"-"`
	Sender    string         `json:"sender"` // 展示名
	SenderID  int64          `json:"-"`
	SenderTyp string         `json:"sender_type"` // user | bot
	MsgType   string         `json:"msgtype"`
	Content   map[string]any `json:"content"`
	Mentioned []string       `json:"mentioned,omitempty"`
	CreatedAt int64          `json:"ts"`
}

// InsertMessage 落库一条消息，msgid 由此生成。
func (s *Store) InsertMessage(chatID, senderID int64, senderType, msgType string, content map[string]any, mentioned []string) (Message, error) {
	cj, err := json.Marshal(content)
	if err != nil {
		return Message{}, err
	}
	var mj any
	if len(mentioned) == 0 {
		mj = ""
	} else {
		if mj, err = json.Marshal(mentioned); err != nil {
			return Message{}, err
		}
	}
	m := Message{ChatID: chatID, SenderID: senderID, SenderTyp: senderType, MsgType: msgType, Content: content, Msgid: NewMsgID()}
	res, err := s.db.Exec(`INSERT INTO message(msgid, chat_id, sender_type, sender_id, msg_type, content_json, mentioned, created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		m.Msgid, chatID, senderType, senderID, msgType, string(cj), mj, now())
	if err != nil {
		return m, err
	}
	m.ID, _ = res.LastInsertId()
	if err := s.db.QueryRow(`SELECT created_at FROM message WHERE id=?`, m.ID).Scan(&m.CreatedAt); err != nil {
		return m, err
	}
	return m, nil
}

// ListMessages 升序返回某群最近 limit 条消息（limit<=0 取 100）。
func (s *Store) ListMessages(chatID int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT m.id, m.msgid, m.sender_type, m.sender_id, m.msg_type, m.content_json, m.mentioned, m.created_at,
		       COALESCE(u.name, b.name, '?')
		FROM message m
		LEFT JOIN "user" u ON m.sender_type='user' AND u.id = m.sender_id
		LEFT JOIN bot     b ON m.sender_type='bot'  AND b.id = m.sender_id
		WHERE m.chat_id = ?
		ORDER BY m.id DESC LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var cj, mstr string
		if err := rows.Scan(&m.ID, &m.Msgid, &m.SenderTyp, &m.SenderID, &m.MsgType, &cj, &mstr, &m.CreatedAt, &m.Sender); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(cj), &m.Content); err != nil {
			return nil, err
		}
		if mstr != "" {
			_ = json.Unmarshal([]byte(mstr), &m.Mentioned)
		}
		out = append(out, m)
	}
	// 反转为升序
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ChatBots 列出群内机器人（@解析与回调路由用）。
func (s *Store) ChatBots(chatID int64) ([]Bot, error) {
	rows, err := s.db.Query(`
		SELECT `+botCols+` FROM chat_bot cb JOIN bot b ON b.id = cb.bot_id WHERE cb.chat_id = ?`, chatID)
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

// CreateUser 创建新用户并加入默认群（自动注册，客户端"昵称登录"用）。
func (s *Store) CreateUser(corpID int64, name string) (User, error) {
	userid := "u" + NewRandomString(6)
	_, err := s.db.Exec(`INSERT INTO "user"(corp_id, userid, name, created_at) VALUES(?,?,?,?)`,
		corpID, userid, name, now())
	if err != nil {
		return User{}, err
	}
	u, err := s.GetUserByUserid(userid)
	if err != nil {
		return u, err
	}
	// 加入默认群（M1a 单群模型）
	var chatID int64
	if err := s.db.QueryRow(`SELECT id FROM chat ORDER BY id LIMIT 1`).Scan(&chatID); err == nil {
		_, _ = s.db.Exec(`INSERT OR IGNORE INTO chat_member(chat_id, user_id) VALUES(?,?)`, chatID, u.ID)
	}
	return u, nil
}

// ListUsers 列出全部用户（预置用户选择）。
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, corp_id, userid, name FROM "user" ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.CorpID, &u.Userid, &u.Name); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// FirstChatOfUser 返回用户所在的第一个群。
func (s *Store) FirstChatOfUser(userID int64) (Chat, error) {
	var c Chat
	err := s.db.QueryRow(`
		SELECT c.id, c.chatid, c.name FROM chat_member m JOIN chat c ON c.id = m.chat_id
		WHERE m.user_id = ? ORDER BY c.id LIMIT 1`, userID).Scan(&c.ID, &c.Chatid, &c.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// TouchMedia 预留素材表写入口（M2 使用，避免迁移时遗漏）。
var _ = time.Now
