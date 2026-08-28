package store

import (
	"database/sql"
	"errors"
)

// CreateOAuthCode 签发一次性 OAuth2 授权码（M3）：绑定 userid，ttl 秒后过期。
func (s *Store) CreateOAuthCode(userid string, ttl int64) (string, error) {
	code := NewRandomString(24)
	_, err := s.db.Exec(`INSERT INTO oauth_code(code, userid, expires_at, created_at) VALUES(?,?,?,?)`,
		code, userid, now()+ttl, now())
	if err != nil {
		return "", err
	}
	return code, nil
}

// ConsumeOAuthCode 校验并占用授权码（一次性）：合法且未过期返回 userid。
func (s *Store) ConsumeOAuthCode(code string) (string, error) {
	var userid string
	var used int
	var exp int64
	if err := s.db.QueryRow(`SELECT userid, used, expires_at FROM oauth_code WHERE code=?`, code).
		Scan(&userid, &used, &exp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("code invalid")
		}
		return "", err
	}
	if used == 1 {
		return "", errors.New("code already used")
	}
	if exp < now() {
		return "", errors.New("code expired")
	}
	if _, err := s.db.Exec(`UPDATE oauth_code SET used=1 WHERE code=?`, code); err != nil {
		return "", err
	}
	return userid, nil
}

// GetChatByChatid 按企微 chatid 取会话（appchat/send 定位群聊用）。
func (s *Store) GetChatByChatid(chatid string) (Chat, error) {
	var c Chat
	err := s.db.QueryRow(`SELECT id, chatid, name, type, agent_id, bot_id FROM chat WHERE chatid=?`, chatid).
		Scan(&c.ID, &c.Chatid, &c.Name, &c.Type, &c.AgentID, &c.BotID)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// GetOrCreateGroupChat 按 chatid 取应用群聊；不存在则创建（appchat/send 语义：
// 应用指定 chatid 的群，首次发送自动建群）。
func (s *Store) GetOrCreateGroupChat(chatid, name string) (Chat, bool, error) {
	if c, err := s.GetChatByChatid(chatid); err == nil {
		return c, false, nil
	}
	if name == "" {
		name = "应用群聊"
	}
	res, err := s.db.Exec(`INSERT INTO chat(chatid, name, created_at) VALUES(?,?,?)`,
		chatid, name, now())
	if err != nil {
		return Chat{}, false, err
	}
	id, _ := res.LastInsertId()
	c, err := s.GetChat(id)
	return c, true, err
}
