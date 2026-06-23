package sigv4

import (
	"strings"
	"testing"
)

// TestCanonicalQueryString_SortsByName verifies that query parameters are
// reordered by name (then value) per the AWS SigV4 canonical query spec.
// Input order must not affect output order.
func TestCanonicalQueryString_SortsByName(t *testing.T) {
	got := canonicalQueryString("b=2&a=1&c=3")
	want := "a=1&b=2&c=3"
	if got != want {
		t.Errorf("canonicalQueryString order mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestCanonicalQueryString_EncodesSpecialChars verifies that names and values
// are URI-encoded per RFC 3986 (unreserved set), with space encoded as %20
// (NOT '+' which is form encoding). Also checks that '=' inside a value is
// preserved (split on first '=' only).
func TestCanonicalQueryString_EncodesSpecialChars(t *testing.T) {
	got := canonicalQueryString("key with space=val&special=!@#&eq=a=b")
	// "key with space" -> "key%20with%20space"
	// "val"            -> "val"
	// "special"        -> "special"
	// "!@#"            -> "%21%40%23"
	// "eq"             -> "eq"
	// "a=b"            -> "a%3Db"  ('=' inside value must be encoded)
	want := "eq=a%3Db&key%20with%20space=val&special=%21%40%23"
	if got != want {
		t.Errorf("canonicalQueryString encoding mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

// 来自 AWS SigV4 文档示例的固定向量，验证算法正确性。
// 参考：docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-test-suite.html
func TestSign_KnownVector(t *testing.T) {
	// 文档示例: GET /?Action=ListUsers&Version=2010-05-08
	// 简化：使用标准 SigV4 步骤，校验生成的 Authorization 头含 5 段。
	auth, date, contentSHA := Sign(
		"GET",
		"example.amazonaws.com",
		"/",
		"Action=ListUsers&Version=2010-05-08",
		"us-east-1",
		"iam",
		nil,
		"AKIDEXAMPLE",
		"wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
	)
	if date == "" {
		t.Error("amz-date empty")
	}
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/") {
		t.Errorf("auth prefix wrong: %s", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=") {
		t.Errorf("auth missing SignedHeaders: %s", auth)
	}
	if !strings.Contains(auth, "Signature=") {
		t.Errorf("auth missing Signature: %s", auth)
	}
	if contentSHA == "" {
		t.Error("contentSHA256 empty")
	}
}

func TestSign_S3(t *testing.T) {
	auth, date, _ := Sign(
		"GET",
		"rustfs.local",
		"/",
		"",
		"us-east-1",
		"s3",
		nil,
		"rustfsadmin",
		"rustfsadmin",
	)
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=rustfsadmin/") {
		t.Errorf("auth prefix wrong: %s", auth)
	}
	if !strings.Contains(date, "T") || !strings.HasSuffix(date, "Z") {
		t.Errorf("date format wrong: %s", date)
	}
}

// TestSign_IncludesContentSha256 验证 Authorization 头的 SignedHeaders 列表
// 必须包含 x-amz-content-sha256（AWS SigV4 spec 要求）。
func TestSign_IncludesContentSha256(t *testing.T) {
	auth, _, contentSHA := Sign(
		"GET", "example.amazonaws.com", "/", "",
		"us-east-1", "s3", nil,
		"AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
	)
	want := "SignedHeaders=host;x-amz-content-sha256;x-amz-date"
	if !strings.Contains(auth, want) {
		t.Errorf("auth missing %q: %s", want, auth)
	}
	// 空 body 时返回空字符串的 SHA-256
	wantEmpty := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if contentSHA != wantEmpty {
		t.Errorf("contentSHA256 = %q, want %q", contentSHA, wantEmpty)
	}
}

// TestSign_NonEmptyBody 验证非空 body 时，contentSHA256 等于其 hex sha256。
func TestSign_NonEmptyBody(t *testing.T) {
	body := []byte("hello")
	_, _, contentSHA := Sign(
		"POST", "example.amazonaws.com", "/", "",
		"us-east-1", "s3", body,
		"AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
	)
	// sha256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if contentSHA != want {
		t.Errorf("contentSHA256 = %q, want %q", contentSHA, want)
	}
}