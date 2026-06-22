package sigv4

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
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

// canonicalQueryString implements the AWS SigV4 canonical query algorithm
// (https://docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-query-string-auth.html#ConstructingTheCanonicalizedQueryString):
//   1. Split on '&'.
//   2. Split each pair on the first '=' only (values may contain '=').
//   3. URI-encode name and value per RFC 3986 unreserved set, with space as %20.
//   4. Sort lexicographically by encoded name, then by encoded value.
//   5. Rejoin with '&'.
func canonicalQueryString(query string) string {
	if query == "" {
		return ""
	}
	pairs := strings.Split(query, "&")
	type kv struct{ k, v string }
	parsed := make([]kv, 0, len(pairs))
	for _, p := range pairs {
		name, value, _ := strings.Cut(p, "=")
		parsed = append(parsed, kv{encodeRFC3986(name), encodeRFC3986(value)})
	}
	sort.Slice(parsed, func(i, j int) bool {
		if parsed[i].k != parsed[j].k {
			return parsed[i].k < parsed[j].k
		}
		return parsed[i].v < parsed[j].v
	})
	parts := make([]string, len(parsed))
	for i, p := range parsed {
		parts[i] = p.k + "=" + p.v
	}
	return strings.Join(parts, "&")
}

// encodeRFC3986 URL-encodes s per RFC 3986, with space encoded as %20
// (NOT '+' which is application/x-www-form-urlencoded). Built on top of
// url.QueryEscape (which already uses %20 for space and correctly escapes
// the unreserved set minus a few chars) plus a follow-up replacement of any
// stray '+' characters that might appear in the input.
func encodeRFC3986(s string) string {
	// First normalize any literal '+' in the input by escaping it, then
	// delegate to url.QueryEscape which encodes space as '+'. Finally swap
	// the form-encoded '+' back to '%20' as required by the SigV4 spec.
	escaped := url.QueryEscape(strings.ReplaceAll(s, "+", "%2B"))
	return strings.ReplaceAll(escaped, "+", "%20")
}
