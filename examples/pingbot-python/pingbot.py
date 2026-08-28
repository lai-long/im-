#!/usr/bin/env python3
"""pingbot-python: 最小“接入方”示例（企微智能机器人 URL 回调模式）。

本示例不复用本平台的任何代码，按企微官方加解密规范独立实现（AES-256-CBC + PKCS7 +
SHA1 签名），用于验证“非 Go 语言的接入方也能与本平台互通”。

依赖：
    pip install cryptography

运行：
    python3 examples/pingbot-python/pingbot.py --token <Token> --aeskey <EncodingAESKey>

然后在平台控制台把回调 URL 填为 http://127.0.0.1:9000/wecom（Token/AESKey 与此处一致），
保存时触发 URL 验证握手；之后在群里 @机器人 即可收到回调，本示例以一次性 stream 回复。
"""

import argparse
import base64
import hashlib
import json
import os
import struct
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qs, urlparse

from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes

BLOCK = 32


class CryptError(Exception):
    pass


def aes_key(encoding_aes_key: str) -> bytes:
    key = base64.b64decode(encoding_aes_key + "=")
    if len(key) != 32:
        raise CryptError("invalid EncodingAESKey")
    return key


def pkcs7_pad(data: bytes) -> bytes:
    n = BLOCK - len(data) % BLOCK
    return data + bytes([n]) * n


def pkcs7_unpad(data: bytes) -> bytes:
    n = data[-1]
    if not 1 <= n <= BLOCK or data[-n:] != bytes([n]) * n:
        raise CryptError("bad padding")
    return data[:-n]


def encrypt(plaintext: str, encoding_aes_key: str, receiveid: str = "") -> str:
    """智能机器人场景 receiveid 为空字符串。"""
    key = aes_key(encoding_aes_key)
    msg = plaintext.encode()
    buf = os.urandom(16) + struct.pack("!I", len(msg)) + msg + receiveid.encode()
    enc = Cipher(algorithms.AES(key), modes.CBC(key[:16])).encryptor()
    return base64.b64encode(enc.update(pkcs7_pad(buf)) + enc.finalize()).decode()


def decrypt(encrypted: str, encoding_aes_key: str, expect_receiveid: str = "") -> str:
    key = aes_key(encoding_aes_key)
    raw = base64.b64decode(encrypted)
    if len(raw) < BLOCK or len(raw) % 16:
        raise CryptError("bad ciphertext")
    dec = Cipher(algorithms.AES(key), modes.CBC(key[:16])).decryptor()
    plain = pkcs7_unpad(dec.update(raw) + dec.finalize())
    if len(plain) < 20:
        raise CryptError("too short")
    (msg_len,) = struct.unpack("!I", plain[16:20])
    content = plain[20 : 20 + msg_len].decode()
    receiveid = plain[20 + msg_len :].decode()
    if expect_receiveid and receiveid != expect_receiveid:
        raise CryptError(f"receiveid mismatch: {receiveid!r}")
    return content


def signature(token: str, timestamp: str, nonce: str, encrypted: str) -> str:
    s = "".join(sorted([token, timestamp, nonce, encrypted]))
    return hashlib.sha1(s.encode()).hexdigest()


class Handler(BaseHTTPRequestHandler):
    token = ""
    aeskey = ""

    def log_message(self, fmt, *args):
        print(f"[pingbot] {fmt % args}")

    def _query(self):
        return {k: v[0] for k, v in parse_qs(urlparse(self.path).query).items()}

    def do_GET(self):
        q = self._query()
        try:
            if signature(self.token, q["timestamp"], q["nonce"], q["echostr"]) != q["msg_signature"]:
                raise CryptError("signature mismatch")
            plain = decrypt(q["echostr"], self.aeskey)
        except (KeyError, CryptError) as exc:
            self.send_error(400, f"verify failed: {exc}")
            return
        print("[pingbot] URL 验证通过")
        body = plain.encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)  # 明文回显，无引号/BOM/换行

    def do_POST(self):
        q = self._query()
        length = int(self.headers.get("Content-Length", 0))
        env = json.loads(self.rfile.read(length) or b"{}")
        try:
            enc = env["encrypt"]
            if signature(self.token, q["timestamp"], q["nonce"], enc) != q["msg_signature"]:
                raise CryptError("signature mismatch")
            msg = json.loads(decrypt(enc, self.aeskey))
        except (KeyError, CryptError, ValueError) as exc:
            self.send_error(400, f"decrypt failed: {exc}")
            return

        print(f"[pingbot] 收到回调: {json.dumps(msg, ensure_ascii=False)[:400]}")
        content = msg.get("text", {}).get("content", "")
        reply = {
            "msgtype": "stream",
            "stream": {"id": msg.get("msgid", "1"), "finish": True,
                       "content": f"**pingbot(python)** 收到：{content}"},
        }
        # 被动回复同样用加密信封返回（明文模式时直接写明文 JSON）
        out = json.dumps({"encrypt": encrypt(json.dumps(reply, ensure_ascii=False), self.aeskey)}
                         ).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(out)))
        self.end_headers()
        self.wfile.write(out)


def main():
    ap = argparse.ArgumentParser(description="企微智能机器人最小接入方示例（Python）")
    ap.add_argument("--port", type=int, default=9000)
    ap.add_argument("--token", required=True, help="平台控制台生成的回调 Token")
    ap.add_argument("--aeskey", required=True, help="平台控制台生成的 EncodingAESKey")
    args = ap.parse_args()

    Handler.token = args.token
    Handler.aeskey = args.aeskey
    print(f"[pingbot] 监听 :{args.port}/wecom")
    HTTPServer(("0.0.0.0", args.port), Handler).serve_forever()


if __name__ == "__main__":
    main()
