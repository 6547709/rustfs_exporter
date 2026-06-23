package rustfs

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

// TLSOptions 控制 HTTPS 客户端的证书校验行为。
//   CACertPath：指向一个 PEM 文件（可包含一个或多个 CA 证书），将其追加到系统信任池。
//     空字符串表示不附加任何证书，使用系统默认池（distroless 镜像内为空）。
//   SkipVerify：跳过 TLS 校验，仅用于测试。
type TLSOptions struct {
	CACertPath string
	SkipVerify bool
}

// BuildHTTPClient 构造一个 HTTP 客户端：
//   - 若 SkipVerify：跳过 TLS 校验。
//   - 若 CACertPath 非空：把 PEM 里的证书追加到系统池（或新建池）。
//   - 否则使用系统默认证书池。
func BuildHTTPClient(opts TLSOptions, timeout time.Duration) (*http.Client, error) {
	transport := &http.Transport{TLSClientConfig: &tls.Config{}}

	switch {
	case opts.SkipVerify:
		transport.TLSClientConfig.InsecureSkipVerify = true
	case opts.CACertPath != "":
		pem, err := os.ReadFile(opts.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("read CA cert %q: %w", opts.CACertPath, err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			// 系统池不可用（如 distroless 镜像），新建一个空池
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no valid PEM certificates in %q", opts.CACertPath)
		}
		transport.TLSClientConfig.RootCAs = pool
	default:
		// 系统默认池，无需额外配置
	}

	return &http.Client{Timeout: timeout, Transport: transport}, nil
}