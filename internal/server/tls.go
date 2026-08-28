package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"im-/internal/config"
)

// EnsureSelfSignedCert 在数据目录生成（或复用）自签证书，
// 用于兼容强制 https 的接入方 SDK。返回证书文件路径供接入方加入信任。
func EnsureSelfSignedCert(dataDir string) (certFile, keyFile string, err error) {
	certFile = filepath.Join(dataDir, "cert.pem")
	keyFile = filepath.Join(dataDir, "key.pem")
	if _, e1 := os.Stat(certFile); e1 == nil {
		if _, e2 := os.Stat(keyFile); e2 == nil {
			return certFile, keyFile, nil // 复用已有证书，避免接入方反复信任
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "local-im-server", Organization: []string{"local-im"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	if ip := localIP(); ip != nil {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

// Serve 以 HTTP 或 HTTPS 启动服务。
func Serve(addr string, handler http.Handler, cfg *config.Config, dataDir string) error {
	if !cfg.TLS {
		return http.ListenAndServe(addr, handler)
	}
	certFile, keyFile, err := EnsureSelfSignedCert(dataDir)
	if err != nil {
		return fmt.Errorf("生成自签证书失败: %w", err)
	}
	log.Printf("TLS 已开启（自签证书）：%s", certFile)
	fmt.Printf("注意：接入方需信任自签证书（可把 %s 加入信任，或在开发期跳过校验）\n", certFile)
	return (&http.Server{
		Addr:              addr,
		Handler:           handler,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
	}).ListenAndServeTLS(certFile, keyFile)
}

// localIP 取一个本机非回环 IPv4 地址（写入证书 SAN）。
func localIP() net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if v4 := ipNet.IP.To4(); v4 != nil {
			return v4
		}
	}
	return nil
}
