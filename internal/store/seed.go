package store

import (
	"database/sql"
	"errors"
	"log"
	"time"
)

// 示例机器人在默认群内的 webhook key，固定可预测（方案文档 §6.1 开箱即用）。
const SeedWebhookKey = "693a91f6-7e79-4f1f-9c39-8a1f0d1f5b6c"

// seedIfEmpty 首次建库时初始化默认企业、群、用户与示例机器人。
func (s *Store) seedIfEmpty() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM corp`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	log.Printf("首次启动：初始化默认企业、默认群与示例机器人")
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()

	res, err := tx.Exec(`INSERT INTO corp(corpid, name, created_at) VALUES(?,?,?)`,
		NewCorpid(), "本地开发企业", now)
	if err != nil {
		return err
	}
	corpID, _ := res.LastInsertId()

	seedUser := func(userid, name string) (int64, error) {
		r, err := tx.Exec(`INSERT INTO "user"(corp_id, userid, name, created_at) VALUES(?,?,?,?)`,
			corpID, userid, name, now)
		if err != nil {
			return 0, err
		}
		return r.LastInsertId()
	}
	u1, err := seedUser("zhangsan", "张三")
	if err != nil {
		return err
	}
	u2, err := seedUser("lisi", "李四")
	if err != nil {
		return err
	}

	r, err := tx.Exec(`INSERT INTO chat(chatid, name, created_at) VALUES(?,?,?)`,
		NewChatID(), "默认群", now)
	if err != nil {
		return err
	}
	chatID, _ := r.LastInsertId()
	for _, uid := range []int64{u1, u2} {
		if _, err := tx.Exec(`INSERT INTO chat_member(chat_id, user_id) VALUES(?,?)`, chatID, uid); err != nil {
			return err
		}
	}

	r, err = tx.Exec(`INSERT INTO bot(corp_id, aibotid, name, callback_token, callback_aes_key, secret, created_at) VALUES(?,?,?,?,?,?,?)`,
		corpID, NewAibotid(), "示例机器人", NewToken(), NewEncodingAESKey(), NewSecret(), now)
	if err != nil {
		return err
	}
	botID, _ := r.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO chat_bot(chat_id, bot_id, webhook_key) VALUES(?,?,?)`,
		chatID, botID, SeedWebhookKey); err != nil {
		return err
	}

	// 示例自建应用（M2：gettoken / message/send / XML 回调）
	if _, err := tx.Exec(`INSERT INTO agent(corp_id, agentid, name, corpsecret, callback_token, callback_aes_key, created_at)
		VALUES(?,?,?,?,?,?,?)`, corpID, 1000002, "示例应用",
		NewSecret(), NewToken(), NewEncodingAESKey(), time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// SeedAgentInfo 返回示例自建应用的接入信息（启动日志打印，开箱即用）。
type SeedAgentInfo struct {
	Corpid    string
	AgentName string
	Agentid   int64
	Secret    string
}

func (s *Store) SeedAgentInfo() (SeedAgentInfo, error) {
	var info SeedAgentInfo
	err := s.db.QueryRow(`
		SELECT c.corpid, a.name, a.agentid, a.corpsecret
		FROM agent a JOIN corp c ON c.id = a.corp_id
		WHERE a.agentid = 1000002`).Scan(&info.Corpid, &info.AgentName, &info.Agentid, &info.Secret)
	if errors.Is(err, sql.ErrNoRows) {
		return info, ErrNotFound
	}
	return info, err
}
