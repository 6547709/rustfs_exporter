package rustfs

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildHTTPClient_Default(t *testing.T) {
	c, err := BuildHTTPClient(TLSOptions{}, 5*time.Second)
	if err != nil {
		t.Fatalf("BuildHTTPClient: %v", err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
}

func TestBuildHTTPClient_SkipVerify(t *testing.T) {
	c, err := BuildHTTPClient(TLSOptions{SkipVerify: true}, 5*time.Second)
	if err != nil {
		t.Fatalf("BuildHTTPClient: %v", err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
}

func TestBuildHTTPClient_CACertValid(t *testing.T) {
	// Generate a self-signed cert in PEM form for testing the parse path.
	pool := x509.NewCertPool()
	if _, err := os.Stat("/etc/ssl/certs/ca-certificates.crt"); err == nil {
		// 系统 CA bundle 存在时用它做 happy-path 测试
	} else if _, err := os.Stat("/etc/pki/tls/certs/ca-bundle.crt"); err == nil {
		// RHEL 系列位置
	} else {
		t.Skip("no system CA bundle available")
	}
	dir := t.TempDir()
	pemPath := filepath.Join(dir, "ca.pem")
	// 写一份合法的 PEM（空内容应返回 AppendCertsFromPEM=false → 报错）。所以这里
	// 拷贝系统 bundle 的部分内容来做 round-trip。
	data, err := os.ReadFile("/etc/ssl/certs/ca-certificates.crt")
	if err != nil {
		// 兜底：从其他已知位置读取
		if data, err = os.ReadFile("/etc/pki/tls/certs/ca-bundle.crt"); err != nil {
			t.Skipf("no system CA bundle readable: %v", err)
		}
	}
	if len(data) == 0 {
		t.Skip("empty system CA bundle")
	}
	if !pool.AppendCertsFromPEM(data) {
		t.Skip("system bundle has no parseable PEM (skipping)")
	}
	// 抽取第一个 PEM block 写到临时文件
	block, _ := pem.Decode(data)
	if block == nil {
		t.Skip("could not extract a single PEM block")
	}
	if err := os.WriteFile(pemPath, pem.EncodeToMemory(block), 0o644); err != nil {
		t.Fatalf("write tmp pem: %v", err)
	}

	c, err := BuildHTTPClient(TLSOptions{CACertPath: pemPath}, 5*time.Second)
	if err != nil {
		t.Fatalf("BuildHTTPClient: %v", err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
}

func TestBuildHTTPClient_CACertMissing(t *testing.T) {
	_, err := BuildHTTPClient(TLSOptions{CACertPath: "/nonexistent/path.pem"}, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for missing CA file, got nil")
	}
}

func TestBuildHTTPClient_CACertInvalidPEM(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.pem")
	if err := os.WriteFile(p, []byte("not a pem"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := BuildHTTPClient(TLSOptions{CACertPath: p}, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for invalid PEM, got nil")
	}
}