// Package store 是 SQLite 访问层：建库迁移、种子数据与 CRUD。
// 使用 modernc.org/sqlite（纯 Go，免 CGO），产物保持单二进制。
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store 封装 SQLite 连接。
type Store struct {
	db      *sql.DB
	dataDir string
}

// Open 打开（必要时创建）数据目录下的 im.db 并执行迁移与种子初始化。
func Open(dataDir string) (*Store, error) {
	dsn := "file:" + filepath.Join(dataDir, "im.db") +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库: %w", err)
	}
	// 单写者模型，避免 SQLITE_BUSY 争用
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("连接数据库: %w", err)
	}
	s := &Store{db: db, dataDir: dataDir}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	if err := s.seedIfEmpty(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close 关闭连接。
func (s *Store) Close() error { return s.db.Close() }

// DB 暴露底层连接（仅供包内测试）。
func (s *Store) DB() *sql.DB { return s.db }

// DataDir 返回数据目录（素材文件存放于 <dataDir>/media）。
func (s *Store) DataDir() string { return s.dataDir }

// mediaDir 返回素材目录。
func mediaDir(dataDir string) string { return filepath.Join(dataDir, "media") }

// ensureDir 确保目录存在。
func ensureDir(dir string) error { return os.MkdirAll(dir, 0o755) }
