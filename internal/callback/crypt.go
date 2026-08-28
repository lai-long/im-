// Package callback 是回调分发器：加解密、URL 验证、推送队列与重试。
// 加解密规范见 docs/设计文档.md §7.4（官方文档 path/101841）：
// 智能机器人场景 receiveid 为空字符串；信封为 JSON {"encrypt": "..."}。
package callback

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const pkcs7Block = 32

var (
	// ErrValidateSig 验签失败。
	ErrValidateSig = errors.New("callback: signature mismatch")
	// ErrDecrypt 解密失败。
	ErrDecrypt = errors.New("callback: decrypt error")
	// ErrReceiveid receiveid 不匹配。
	ErrReceiveid = errors.New("callback: receiveid mismatch")
)

// AESKey 由 EncodingAESKey（43 位）推导 32 字节密钥。
func AESKey(encodingAESKey string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("%w: invalid EncodingAESKey", ErrDecrypt)
	}
	return key, nil
}

// Signature 计算 msg_signature：sha1(sort(token, timestamp, nonce, encrypt)).
func Signature(token, timestamp, nonce, encrypt string) string {
	parts := []string{token, timestamp, nonce, encrypt}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return fmt.Sprintf("%x", sum)
}

// CheckSignature 校验 msg_signature。
func CheckSignature(token, timestamp, nonce, encrypt, wantSig string) bool {
	return Signature(token, timestamp, nonce, encrypt) == wantSig
}

// Encrypt 按企微规范加密：
// 明文 = random(16B) + msg_len(4B 网络序) + msg + receiveid，PKCS7(32) 填充，
// AES-256-CBC（IV 取密钥前 16 字节），输出 Base64。
func Encrypt(plaintext string, encodingAESKey, receiveid string) (string, error) {
	key, err := AESKey(encodingAESKey)
	if err != nil {
		return "", err
	}
	msg := []byte(plaintext)
	buf := make([]byte, 0, 16+4+len(msg)+len(receiveid)+pkcs7Block)
	var rnd [16]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	buf = append(buf, rnd[:]...)
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(msg)))
	buf = append(buf, l[:]...)
	buf = append(buf, msg...)
	buf = append(buf, receiveid...)
	buf = pkcs7Pad(buf)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	out := make([]byte, len(buf))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(out, buf)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt 解密并返回明文与 receiveid；expectReceiveid 为空串表示不校验（机器人场景应为空串）。
func Decrypt(encryptedBase64 string, encodingAESKey, expectReceiveid string) (string, string, error) {
	key, err := AESKey(encodingAESKey)
	if err != nil {
		return "", "", err
	}
	cipherText, err := base64.StdEncoding.DecodeString(encryptedBase64)
	if err != nil || len(cipherText) < pkcs7Block || len(cipherText)%aes.BlockSize != 0 {
		return "", "", fmt.Errorf("%w: bad ciphertext", ErrDecrypt)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	plain := make([]byte, len(cipherText))
	cipher.NewCBCDecrypter(block, key[:16]).CryptBlocks(plain, cipherText)
	plain, err = pkcs7Unpad(plain)
	if err != nil {
		return "", "", err
	}
	if len(plain) < 20 {
		return "", "", fmt.Errorf("%w: too short", ErrDecrypt)
	}
	msgLen := binary.BigEndian.Uint32(plain[16:20])
	if uint32(len(plain)-20) < msgLen {
		return "", "", fmt.Errorf("%w: bad length", ErrDecrypt)
	}
	msg := string(plain[20 : 20+msgLen])
	receiveid := string(plain[20+msgLen:])
	if expectReceiveid != "" && receiveid != expectReceiveid {
		return "", receiveid, fmt.Errorf("%w: got %q want %q", ErrReceiveid, receiveid, expectReceiveid)
	}
	return msg, receiveid, nil
}

// VerifyEchoStr 处理 URL 验证：校验 msg_signature 并解密 echostr，返回应回显的明文。
// 机器人场景 receiveid 为空字符串。调用方须在 1 秒内应答（企微语义）。
func VerifyEchoStr(token, encodingAESKey, timestamp, nonce, echostr, msgSignature string) (string, error) {
	if !CheckSignature(token, timestamp, nonce, echostr, msgSignature) {
		return "", ErrValidateSig
	}
	plain, _, err := Decrypt(echostr, encodingAESKey, "")
	return plain, err
}

func pkcs7Pad(b []byte) []byte {
	n := pkcs7Block - len(b)%pkcs7Block
	return append(b, bytes.Repeat([]byte{byte(n)}, n)...)
}

func pkcs7Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("%w: empty", ErrDecrypt)
	}
	n := int(b[len(b)-1])
	if n == 0 || n > pkcs7Block || n > len(b) {
		return nil, fmt.Errorf("%w: bad padding", ErrDecrypt)
	}
	for _, v := range b[len(b)-n:] {
		if int(v) != n {
			return nil, fmt.Errorf("%w: bad padding", ErrDecrypt)
		}
	}
	return b[:len(b)-n], nil
}
