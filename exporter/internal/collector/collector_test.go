package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/local/rustfs-exporter/internal/config"
	"github.com/local/rustfs-exporter/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newSingleTargetWorker(t *testing.T, _ string, _ string, m *metrics.Collector) *ScrapeWorker {
	t.Helper()
	// dummy — replaced per-test using a combined server
	return nil
}

func TestScrapeWorker_OneCycle(t *testing.T) {
	var bucketCalls, replCalls, healthCalls atomic.Int32

	// 一个 server 同时服务 S3 + admin（同一 endpoint）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			bucketCalls.Add(1)
			w.Write([]byte(`<ListAllMyBucketsResult><Buckets><Bucket><Name>alpha</Name></Bucket></Buckets></ListAllMyBucketsResult>`))
		case r.URL.Path == "/health/ready":
			healthCalls.Add(1)
			w.Write([]byte(`{"ready":true,"details":{"storage":{"ready":true},"iam":{"ready":true},"lock":{"ready":true}}}`))
		case r.URL.Path == "/rustfs/admin/v3/replicationmetrics":
			replCalls.Add(1)
			w.Write([]byte(`{"stats":{"arn:1":{"replicated_size":0,"replicated_count":0,"failed":{"count":0,"size":0},"bandwidth_limit_bytes_per_sec":0,"current_bandwidth_bytes_per_sec":0}},"replica_size":1,"replica_count":0,"replicated_size":0,"replicated_count":0,"q_stat":{"curr":{"bytes":0,"count":0},"avg":{"bytes":0,"count":0},"max":{"bytes":0,"count":0},"last_minute":{"bytes":0,"count":0}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := metrics.NewCollector()
	reg := prometheus.NewRegistry()
	c.Register(reg)
	c.SetOverallUp(true)

	w, err := NewScrapeWorker([]config.Target{{
		Name: "main-cluster", Endpoint: srv.URL, AccessKey: "a", SecretKey: "b",
	}}, c, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewScrapeWorker: %v", err)
	}

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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 500)
	}))
	defer srv.Close()

	c := metrics.NewCollector()
	reg := prometheus.NewRegistry()
	c.Register(reg)

	w, err := NewScrapeWorker([]config.Target{{
		Name: "main-cluster", Endpoint: srv.URL, AccessKey: "a", SecretKey: "b",
	}}, c, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewScrapeWorker: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	if v := testutil.ToFloat64(c.Up); v != 0 {
		t.Errorf("up=%v, want 0", v)
	}
}

func TestScrapeWorker_MultiTarget(t *testing.T) {
	var targetACalls, targetBCalls atomic.Int32

	targetA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetACalls.Add(1)
		w.Write([]byte(`<ListAllMyBucketsResult><Buckets><Bucket><Name>bucketA</Name></Bucket></Buckets></ListAllMyBucketsResult>`))
	}))
	defer targetA.Close()

	targetB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetBCalls.Add(1)
		// 目标 rustfs 不配置复制 → 返回 404
		http.NotFound(w, r)
	}))
	defer targetB.Close()

	// targetA 同时也是 admin（同一测试服务器处理两个路径）
	adminHandler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health/ready":
			w.Write([]byte(`{"ready":true,"details":{"storage":{"ready":true}}}`))
		case "/rustfs/admin/v3/replicationmetrics":
			w.Write([]byte(`{"stats":{"arn:1":{"replicated_size":42,"replicated_count":7,"failed":{"count":0,"size":0},"bandwidth_limit_bytes_per_sec":0,"current_bandwidth_bytes_per_sec":1234.5}},"replica_size":0,"replica_count":0,"replicated_size":42,"replicated_count":7,"q_stat":{"curr":{"bytes":1,"count":0},"avg":{"bytes":0,"count":0},"max":{"bytes":2,"count":0},"last_minute":{"bytes":0,"count":0}}}`))
		default:
			http.NotFound(w, r)
		}
	}
	adminA := httptest.NewServer(http.HandlerFunc(adminHandler))
	defer adminA.Close()

	// targetA 用一个 server（同时是 S3 和 admin），targetB 只用 S3（admin 也会 404）
	// S3 服务：因为 adminA 也得能响应 ListBuckets 我们用同一个 server
	s3AndAdminA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			targetACalls.Add(1)
			w.Write([]byte(`<ListAllMyBucketsResult><Buckets><Bucket><Name>bucketA</Name></Bucket></Buckets></ListAllMyBucketsResult>`))
			return
		}
		adminHandler(w, r)
	}))
	defer s3AndAdminA.Close()

	// 替换 targetA — 用同一个 s3AndAdminA
	_ = targetA

	c := metrics.NewCollector()
	reg := prometheus.NewRegistry()
	c.Register(reg)

	w, err := NewScrapeWorker([]config.Target{
		{Name: "source", Endpoint: s3AndAdminA.URL, AccessKey: "a", SecretKey: "b"},
		{Name: "target", Endpoint: targetB.URL, AccessKey: "c", SecretKey: "d"},
	}, c, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewScrapeWorker: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	if targetACalls.Load() == 0 {
		t.Error("source target not scraped")
	}
	if targetBCalls.Load() == 0 {
		t.Error("target target not scraped")
	}

	// 验证 source/bucketA 的 completed_bytes = 42
	if v := testutil.ToFloat64(c.CompletedBytes.WithLabelValues("source", "bucketA")); v != 42 {
		t.Errorf("source/bucketA completed_bytes=%v, want 42", v)
	}
	// 验证 target/bucketA 不存在（target 没配复制，404 跳过）
	if v := testutil.ToFloat64(c.CompletedBytes.WithLabelValues("target", "bucketA")); v != 0 {
		t.Errorf("target/bucketA should be 0 (not set), got %v", v)
	}
	// 验证整体 Up=1（任一 instance 通即 1）
	if v := testutil.ToFloat64(c.Up); v != 1 {
		t.Errorf("up=%v, want 1 (any target up)", v)
	}
}