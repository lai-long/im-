package store

import (
	"testing"
)

func TestOpenMigrateSeed(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// 二次打开幂等
	s2, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	s2.Close()

	var corpN int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM corp`).Scan(&corpN); err != nil || corpN != 1 {
		t.Fatalf("corp count=%d err=%v", corpN, err)
	}

	chat, bot, err := s.GetChatByWebhookKey(SeedWebhookKey)
	if err != nil {
		t.Fatalf("GetChatByWebhookKey: %v", err)
	}
	if chat.Name != "默认群" || bot.Name != "示例机器人" {
		t.Fatalf("seed 不符: chat=%+v bot=%+v", chat, bot)
	}

	info, err := s.SeedWebhookInfo()
	if err != nil {
		t.Fatalf("SeedWebhookInfo: %v", err)
	}
	if len(info.CallbackToken) == 0 || len(info.CallbackAESKey) != 43 {
		t.Fatalf("回调三元组生成异常: %+v", info)
	}

	if _, err := s.GetUserByUserid("zhangsan"); err != nil {
		t.Fatalf("GetUserByUserid: %v", err)
	}
	if _, _, err := s.GetChatByWebhookKey("not-exist"); err != ErrNotFound {
		t.Fatalf("期望 ErrNotFound, got %v", err)
	}

	// msgid 形态与唯一性
	m1, m2 := NewMsgID(), NewMsgID()
	if m1 == m2 {
		t.Fatal("msgid 冲突")
	}
	if len(NewToken()) > 32 || len(NewEncodingAESKey()) != 43 || len(NewChatID()) < 3 {
		t.Fatal("ID 生成器形态异常")
	}
}
