package callback

import (
	"encoding/base64"
	"encoding/xml"
	"strings"
	"testing"

	official "github.com/sbzhu/weworkapi_golang/wxbizmsgcrypt"
)

// 本测试与企微官方示例库（sbzhu/weworkapi_golang）做双向互通验证：
// 我方加密 → 官方解密；官方加密 → 我方解密。
// 机器人场景 receiveid 为空字符串（设计文档 §7.4）。

const (
	testToken  = "QDG6eK"
	testAESKey = "jWmYm7qr9nMoAVwEjSiPJBmA1CQvL47GfIxCZWvAfdP" // 43 位
	testTS     = "1409659813"
	testNonce  = "1372623149"
	// 智能机器人场景 receiveid 为空
	testReceiveid = ""
)

func officialCrypt() *official.WXBizMsgCrypt {
	return official.NewWXBizMsgCrypt(testToken, testAESKey, testReceiveid, official.XmlType)
}

func TestRoundtripSelf(t *testing.T) {
	plain := `<json>{"msgtype":"text","text":{"content":"@bot hello"}}</json>`
	enc, err := Encrypt(plain, testAESKey, testReceiveid)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, rid, err := Decrypt(enc, testAESKey, testReceiveid)
	if err != nil || got != plain || rid != testReceiveid {
		t.Fatalf("Decrypt roundtrip: got=%q rid=%q err=%v", got, rid, err)
	}
}

func TestSignatureStable(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString([]byte("whatever"))
	want := Signature(testToken, testTS, testNonce, enc)
	// 与官方 calSignature 同算法：排序拼接 sha1
	if got := Signature(testToken, testTS, testNonce, enc); got != want || len(want) != 40 {
		t.Fatalf("signature 不稳定或形态异常: %s", want)
	}
}

func TestOurEncryptOfficialDecrypt(t *testing.T) {
	plain := `<json>{"msgtype":"text","text":{"content":"ping"}}</json>`
	enc, err := Encrypt(plain, testAESKey, testReceiveid)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	sig := Signature(testToken, testTS, testNonce, enc)
	body := `<xml><Encrypt><![CDATA[` + enc + `]]></Encrypt></xml>`
	msg, cerr := officialCrypt().DecryptMsg(sig, testTS, testNonce, []byte(body))
	if cerr != nil {
		t.Fatalf("官方库解密我方密文失败: %+v", cerr)
	}
	if !strings.Contains(string(msg), "ping") {
		t.Fatalf("官方解密结果不含原文: %q", msg)
	}
}

type officialEnvelope struct {
	Encrypt     string `xml:"Encrypt"`
	MsgSignature string `xml:"MsgSignature"`
	TimeStamp   string `xml:"TimeStamp"`
	Nonce       string `xml:"Nonce"`
}

func TestOfficialEncryptOurDecrypt(t *testing.T) {
	plain := `<json>{"msgtype":"text","text":{"content":"pong"}}</json>`
	raw, cerr := officialCrypt().EncryptMsg(plain, testTS, testNonce)
	if cerr != nil {
		t.Fatalf("官方加密失败: %+v", cerr)
	}
	var env officialEnvelope
	if err := xml.Unmarshal(raw, &env); err != nil {
		t.Fatalf("解析官方信封: %v", err)
	}
	if !CheckSignature(testToken, env.TimeStamp, env.Nonce, env.Encrypt, env.MsgSignature) {
		t.Fatalf("我方验签官方签名失败")
	}
	got, _, err := Decrypt(env.Encrypt, testAESKey, testReceiveid)
	if err != nil || got != plain {
		t.Fatalf("我方解密官方密文: got=%q err=%v", got, err)
	}
}

func TestEchostrCross(t *testing.T) {
	echoPlain := "7321423-some-plain-echostr"
	enc, err := Encrypt(echoPlain, testAESKey, testReceiveid)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	sig := Signature(testToken, testTS, testNonce, enc)

	// 官方 VerifyURL 解我方 echostr
	got, cerr := officialCrypt().VerifyURL(sig, testTS, testNonce, enc)
	if cerr != nil || string(got) != echoPlain {
		t.Fatalf("官方 VerifyURL: got=%q err=%+v", got, cerr)
	}
	// 我方 VerifyEchoStr 解官方加密的 echostr
	offRaw, cerr := officialCrypt().EncryptMsg(echoPlain, testTS, testNonce)
	if cerr != nil {
		t.Fatalf("官方加密 echostr: %+v", cerr)
	}
	var env officialEnvelope
	_ = xml.Unmarshal(offRaw, &env)
	plain, err := VerifyEchoStr(testToken, testAESKey, env.TimeStamp, env.Nonce, env.Encrypt, env.MsgSignature)
	if err != nil || plain != echoPlain {
		t.Fatalf("我方 VerifyEchoStr: got=%q err=%v", plain, err)
	}
	// 验签失败路径
	if _, err := VerifyEchoStr(testToken, testAESKey, testTS, testNonce, enc, "badsig"); err != ErrValidateSig {
		t.Fatalf("期望 ErrValidateSig, got %v", err)
	}
}

func TestTamperAndReceiveid(t *testing.T) {
	plain := "hello"
	enc, _ := Encrypt(plain, testAESKey, "corp-x")
	if _, _, err := Decrypt(enc, testAESKey, "corp-y"); err == nil {
		t.Fatal("receiveid 不匹配应报错")
	}
	// 错误长度 AESKey（42 位，补 '=' 后解码不足 32 字节）
	if _, _, err := Decrypt(enc, strings.Repeat("A", 42), ""); err == nil {
		t.Fatal("非法 AESKey 应报错")
	}
	// 修改密文末字节（padding 破坏或内容破坏）应报错
	raw, _ := base64.StdEncoding.DecodeString(enc)
	raw[len(raw)-1] ^= 0xff
	bad := base64.StdEncoding.EncodeToString(raw)
	if _, _, err := Decrypt(bad, testAESKey, "corp-x"); err == nil {
		t.Fatal("篡改密文应报错")
	}
}
