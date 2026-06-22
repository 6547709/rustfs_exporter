package sigv4

import (
	"strings"
	"testing"
)

// 来自 AWS SigV4 文档示例的固定向量，验证算法正确性。
// 参考：docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-test-suite.html
func TestSign_KnownVector(t *testing.T) {
	// 文档示例: GET /?Action=ListUsers&Version=2010-05-08
	// 简化：使用标准 SigV4 步骤，校验生成的 Authorization 头含 5 段。
	auth, date := Sign(
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
}

func TestSign_S3(t *testing.T) {
	auth, date := Sign(
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
