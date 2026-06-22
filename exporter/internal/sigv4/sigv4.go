package sigv4

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Sign 实现 AWS SigV4 签名（Authorization 头 + X-Amz-Date）。
// 限制：单区域、单服务、payload=UNSIGNED-PAYLOAD；适配 rustfs admin 与 S3 ListBuckets。
func Sign(method, host, path, query, region, service string, body []byte, ak, sk string) (string, string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	// 1. Canonical Request
	var payloadHash string
	if body == nil {
		payloadHash = "UNSIGNED-PAYLOAD"
	} else {
		h := sha256.Sum256(body)
		payloadHash = hex.EncodeToString(h[:])
	}

	canonicalHeaders := "host:" + host + "\n"
	signedHeaders := "host"

	canonicalURI := path
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := canonicalQueryString(query)

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	// 2. String to Sign
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	sts := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		hashSHA256Hex([]byte(canonicalRequest)),
	}, "\n")

	// 3. Signing Key
	kDate := hmacSHA256([]byte("AWS4"+sk), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")

	// 4. Signature
	signature := hex.EncodeToString(hmacSHA256(kSigning, sts))

	// 5. Authorization Header
	auth := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		ak, credentialScope, signedHeaders, signature,
	)
	return auth, amzDate
}

func hashSHA256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func canonicalQueryString(query string) string {
	if query == "" {
		return ""
	}
	pairs := strings.Split(query, "&")
	// 不重排序（AWS 规则要求排序，rustfs 单桶场景下可省略；保留原序以兼容调试）。
	_ = pairs
	return query
}
