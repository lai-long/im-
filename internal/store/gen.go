package store

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"
)

// NewUUID 生成 UUID v4（webhook key 形态对齐企微）。
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewRandomString 生成 n 字节随机数的 base64 字符串（无填充）。
func NewRandomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawStdEncoding.EncodeToString(b)
}

// NewToken 生成回调 Token（≤32 位，字母数字）。
func NewToken() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(out)
}

// NewEncodingAESKey 生成 43 位 EncodingAESKey（解码时补 '=' 得 32 字节）。
func NewEncodingAESKey() string {
	return base64.RawStdEncoding.EncodeToString(randomBytes(32))
}

// NewMsgID 生成 base64 形态的 msgid：8 字节纳秒时间戳 + 4 字节随机。
// 对齐企微 base64 msgid 的唯一性与排重语义，内容不可解析（设计文档 §1.5）。
func NewMsgID() string {
	var buf [12]byte
	binary.BigEndian.PutUint64(buf[:8], uint64(time.Now().UnixNano()))
	copy(buf[8:], randomBytes(4))
	return base64.StdEncoding.EncodeToString(buf[:])
}

// NewChatID / NewAibotid 生成对齐企微前缀风格的 ID。
func NewChatID() string   { return "wr" + NewRandomString(12) }
func NewAibotid() string  { return "wb" + NewRandomString(12) }
func NewCorpid() string   { return "ww_local_" + NewRandomString(8) }

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
