package rustfs

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/local/rustfs-exporter/internal/sigv4"
)

type S3Client struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	HTTP      *http.Client
}

// NewS3Client 构造一个 S3 客户端。tlsOpts 用于 HTTPS 证书校验配置。
func NewS3Client(ep, region, ak, sk string, tlsOpts TLSOptions) (*S3Client, error) {
	httpClient, err := BuildHTTPClient(tlsOpts, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("s3 client: %w", err)
	}
	return &S3Client{
		Endpoint:  strings.TrimRight(ep, "/"),
		Region:    region,
		AccessKey: ak,
		SecretKey: sk,
		HTTP:      httpClient,
	}, nil
}

type s3ListAllMyBucketsResult struct {
	XMLName xml.Name    `xml:"ListAllMyBucketsResult"`
	Buckets s3BucketLst `xml:"Buckets"`
}
type s3BucketLst struct {
	Bucket []s3Bucket `xml:"Bucket"`
}
type s3Bucket struct {
	Name string `xml:"Name"`
}

func (c *S3Client) ListBuckets(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint+"/", nil)
	if err != nil {
		return nil, err
	}
	auth, date, contentSHA := sigv4.Sign("GET", req.URL.Host, "/", "", c.Region, "s3", nil, c.AccessKey, c.SecretKey)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Amz-Date", date)
	req.Header.Set("X-Amz-Content-Sha256", contentSHA)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ListBuckets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ListBuckets: status %d, body=%s", resp.StatusCode, string(body))
	}

	var out s3ListAllMyBucketsResult
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ListBuckets: decode: %w", err)
	}
	names := make([]string, 0, len(out.Buckets.Bucket))
	for _, b := range out.Buckets.Bucket {
		if b.Name != "" {
			names = append(names, b.Name)
		}
	}
	return names, nil
}