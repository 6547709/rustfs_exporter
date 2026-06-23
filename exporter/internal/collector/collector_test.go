package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/local/rustfs-exporter/internal/metrics"
	"github.com/local/rustfs-exporter/internal/rustfs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newClients(t *testing.T, s3URL, adminURL string) (*rustfs.S3Client, *rustfs.AdminClient) {
	t.Helper()
	s3, err := rustfs.NewS3Client(s3URL, "us-east-1", "a", "b", rustfs.TLSOptions{})
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}
	adm, err := rustfs.NewAdminClient(adminURL, "us-east-1", "a", "b", rustfs.TLSOptions{})
	if err != nil {
		t.Fatalf("NewAdminClient: %v", err)
	}
	return s3, adm
}

func TestScrapeWorker_OneCycle(t *testing.T) {
	var bucketCalls, replCalls, healthCalls atomic.Int32

	s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucketCalls.Add(1)
		w.Write([]byte(`<ListAllMyBucketsResult><Buckets><Bucket><Name>alpha</Name></Bucket></Buckets></ListAllMyBucketsResult>`))
	}))
	defer s3.Close()

	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health/ready":
			healthCalls.Add(1)
			w.Write([]byte(`{"ready":true,"details":{"storage":{"ready":true},"iam":{"ready":true},"lock":{"ready":true}}}`))
		case "/rustfs/admin/v3/replicationmetrics":
			replCalls.Add(1)
			w.Write([]byte(`{"stats":{"arn:1":{"replicated_size":0,"replicated_count":0,"failed":{"count":0,"size":0},"bandwidth_limit_bytes_per_sec":0,"current_bandwidth_bytes_per_sec":0}},"replica_size":1,"replica_count":0,"replicated_size":0,"replicated_count":0,"q_stat":{"curr":{"bytes":0,"count":0},"avg":{"bytes":0,"count":0},"max":{"bytes":0,"count":0},"last_minute":{"bytes":0,"count":0}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer admin.Close()

	c := metrics.NewCollector()
	reg := prometheus.NewRegistry()
	c.Register(reg)
	c.Up.Set(1)

	s3Client, adminClient := newClients(t, s3.URL, admin.URL)
	w := NewScrapeWorker(s3Client, adminClient, c, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	if bucketCalls.Load() == 0 {
		t.Error("no ListBuckets call")
	}
	if replCalls.Load() == 0 {
		t.Error("no replication call")
	}
	if healthCalls.Load() == 0 {
		t.Error("no health call")
	}
	if v := testutil.ToFloat64(c.Up); v != 1 {
		t.Errorf("up=%v", v)
	}
}

func TestScrapeWorker_SetsUpToZeroOnError(t *testing.T) {
	s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 500)
	}))
	defer s3.Close()

	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 500)
	}))
	defer admin.Close()

	c := metrics.NewCollector()
	reg := prometheus.NewRegistry()
	c.Register(reg)

	s3Client, adminClient := newClients(t, s3.URL, admin.URL)
	w := NewScrapeWorker(s3Client, adminClient, c, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	// 一旦 S3 失败，up 应被置 0
	if v := testutil.ToFloat64(c.Up); v != 0 {
		t.Errorf("up=%v, want 0", v)
	}
}