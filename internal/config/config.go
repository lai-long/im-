package config

import (
	"strings"
)

// Config 是 im-server 的启动配置。
type Config struct {
	Addr            string // HTTP 监听地址，如 :7788
	DataDir         string // 数据目录（im.db + media/）
	PublicURL       string // 平台对外可达地址，用于生成 response_url 与启动日志；空则用监听地址
	TLS             bool   // 开启 TLS（自签证书）
	ConsolePassword string // 控制台口令，空表示免鉴权
}

// ExternalBaseURL 返回生成对外地址（response_url、启动日志接入信息）的 Base URL。
func (c *Config) ExternalBaseURL() string {
	if c.PublicURL != "" {
		return strings.TrimRight(c.PublicURL, "/")
	}
	scheme := "http"
	if c.TLS {
		scheme = "https"
	}
	addr := c.Addr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return scheme + "://" + addr
}
