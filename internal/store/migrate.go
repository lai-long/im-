package store

import "fmt"

// migrations 按序号递增；每个版本只执行一次。
// 修改表结构时在末尾追加新版本，不改动历史迁移。
var migrations = []string{
	// v1：M1a 核心表
	`
CREATE TABLE corp(
  id         INTEGER PRIMARY KEY,
  corpid     TEXT UNIQUE NOT NULL,
  name       TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE bot(
  id                INTEGER PRIMARY KEY,
  corp_id           INTEGER NOT NULL REFERENCES corp(id),
  aibotid           TEXT UNIQUE NOT NULL,
  name              TEXT NOT NULL,
  callback_url      TEXT NOT NULL DEFAULT '',
  callback_token    TEXT NOT NULL DEFAULT '',
  callback_aes_key  TEXT NOT NULL DEFAULT '',
  callback_mode     TEXT NOT NULL DEFAULT 'encrypted', -- plain | encrypted
  callback_verified INTEGER NOT NULL DEFAULT 0,
  created_at        INTEGER NOT NULL
);

CREATE TABLE "user"(
  id         INTEGER PRIMARY KEY,
  corp_id    INTEGER NOT NULL REFERENCES corp(id),
  userid     TEXT UNIQUE NOT NULL,
  name       TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE chat(
  id         INTEGER PRIMARY KEY,
  chatid     TEXT UNIQUE NOT NULL,
  name       TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE chat_member(
  chat_id INTEGER NOT NULL REFERENCES chat(id),
  user_id INTEGER NOT NULL REFERENCES "user"(id),
  PRIMARY KEY(chat_id, user_id)
);

CREATE TABLE chat_bot(
  chat_id     INTEGER NOT NULL REFERENCES chat(id),
  bot_id      INTEGER NOT NULL REFERENCES bot(id),
  webhook_key TEXT UNIQUE NOT NULL, -- key 按群分配（企微同款语义）
  PRIMARY KEY(chat_id, bot_id)
);

CREATE TABLE message(
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  msgid        TEXT UNIQUE NOT NULL,
  chat_id      INTEGER NOT NULL REFERENCES chat(id),
  sender_type  TEXT NOT NULL, -- user | bot
  sender_id    INTEGER NOT NULL,
  msg_type     TEXT NOT NULL,
  content_json TEXT NOT NULL,
  mentioned    TEXT NOT NULL DEFAULT '', -- JSON 数组：命中的 userid / "@all"
  created_at   INTEGER NOT NULL
);

CREATE TABLE callback_task(
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  message_id         INTEGER NOT NULL REFERENCES message(id),
  bot_id             INTEGER NOT NULL REFERENCES bot(id),
  payload            TEXT NOT NULL, -- 加密前的明文 JSON
  response_code      TEXT NOT NULL DEFAULT '',
  response_used      INTEGER NOT NULL DEFAULT 0,
  response_expire_at INTEGER NOT NULL DEFAULT 0,
  stream_id          TEXT NOT NULL DEFAULT '',
  stream_finished    INTEGER NOT NULL DEFAULT 0,
  status             TEXT NOT NULL DEFAULT 'pending', -- pending|processing|done|dead
  attempt            INTEGER NOT NULL DEFAULT 0,
  next_retry_at      INTEGER NOT NULL DEFAULT 0,
  last_error         TEXT NOT NULL DEFAULT '',
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL
);

CREATE INDEX idx_callback_task_status ON callback_task(status, next_retry_at);
CREATE INDEX idx_message_chat ON message(chat_id, id);

CREATE TABLE media(
  id         INTEGER PRIMARY KEY,
  media_id   TEXT UNIQUE NOT NULL,
  type       TEXT NOT NULL,
  file_path  TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expire_at  INTEGER NOT NULL
);
`,
	// v2：流式回复——记录流式消息 ID，用于全量刷新（M1b）
	`ALTER TABLE callback_task ADD COLUMN stream_message_id INTEGER NOT NULL DEFAULT 0;`,
	// v3：自建应用（gettoken / message/send / XML 回调）+ 应用单聊会话（M2）
	`
ALTER TABLE chat ADD COLUMN type TEXT NOT NULL DEFAULT 'group';        -- group | direct
ALTER TABLE chat ADD COLUMN agent_id INTEGER NOT NULL DEFAULT 0;       -- direct 会话对应的自建应用

CREATE TABLE agent(
  id                INTEGER PRIMARY KEY,
  corp_id           INTEGER NOT NULL REFERENCES corp(id),
  agentid           INTEGER UNIQUE NOT NULL,
  name              TEXT NOT NULL,
  corpsecret        TEXT UNIQUE NOT NULL,
  callback_url      TEXT NOT NULL DEFAULT '',
  callback_token    TEXT NOT NULL DEFAULT '',
  callback_aes_key  TEXT NOT NULL DEFAULT '',
  callback_mode     TEXT NOT NULL DEFAULT 'encrypted', -- plain | encrypted
  callback_verified INTEGER NOT NULL DEFAULT 0,
  created_at        INTEGER NOT NULL
);

CREATE TABLE token(
  id           INTEGER PRIMARY KEY,
  agent_id     INTEGER NOT NULL REFERENCES agent(id),
  access_token TEXT UNIQUE NOT NULL,
  expires_at   INTEGER NOT NULL,
  created_at   INTEGER NOT NULL
);
CREATE INDEX idx_token_value ON token(access_token);
`,
	// v4：回调任务区分目标（机器人 / 自建应用）
	`ALTER TABLE callback_task ADD COLUMN target_type TEXT NOT NULL DEFAULT 'bot'; -- bot | agent`,
	// v5：机器人单聊（chattype=single），chat 增加 bot_id 列（direct 会话用 agent_id，single 用 bot_id）
	`ALTER TABLE chat ADD COLUMN bot_id INTEGER NOT NULL DEFAULT 0; -- single 会话对应的机器人`,
	// v6：机器人长连接（M3）订阅密钥；有活跃 wss 连接时优先走长连接推送
	`ALTER TABLE bot ADD COLUMN secret TEXT NOT NULL DEFAULT ''; -- 长连接 aibot_subscribe 鉴权密钥`,
	// v7：OAuth2 网页授权（M3）：授权码一次性、短时有效
	`
CREATE TABLE oauth_code(
  code       TEXT PRIMARY KEY,
  userid     TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  used       INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
`,
}

// migrate 依次执行未应用的迁移。
func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version(
  version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("建 schema_version 表: %w", err)
	}
	var current int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_version`).Scan(&current); err != nil {
		return fmt.Errorf("读取 schema 版本: %w", err)
	}
	for i, ddl := range migrations {
		v := i + 1
		if v <= current {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ddl); err != nil {
			tx.Rollback()
			return fmt.Errorf("执行迁移 v%d: %w", v, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version(version, applied_at) VALUES(?, strftime('%s','now'))`, v); err != nil {
			tx.Rollback()
			return fmt.Errorf("记录 schema 版本 v%d: %w", v, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
