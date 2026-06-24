package metrics

import (
	"strings"
	"testing"

	"github.com/local/rustfs-exporter/internal/rustfs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCollector_UpdateReplication(t *testing.T) {
	c := NewCollector()
	reg := prometheus.NewRegistry()
	c.Register(reg)

	c.UpdateReplication("main-cluster", "alpha", &rustfs.ReplicationStats{
		Stats: map[string]rustfs.PerARNStat{
			"arn:1": {
				ReplicatedSize:              500,
				ReplicatedCount:             5,
				Failed:                      rustfs.FailedM{Count: 1, Size: 10},
				CurrentBandwidthBytesPerSec: 1024.0,
			},
		},
		ReplicaSize:     100,
		ReplicaCount:    2,
		ReplicatedSize:  500,
		ReplicatedCount: 5,
		QStat: rustfs.QStat{
			Current:    rustfs.QueueValue{Bytes: 50, Count: 1},
			Average:    rustfs.QueueValue{Bytes: 60, Count: 1},
			Max:        rustfs.QueueValue{Bytes: 80, Count: 2},
			LastMinute: rustfs.QueueValue{Bytes: 200, Count: 3},
		},
	})

	expected := `
# HELP rustfs_replication_pending_bytes Bytes still waiting to be replicated (current backlog size). Unit: bytes.
# TYPE rustfs_replication_pending_bytes gauge
rustfs_replication_pending_bytes{bucket="alpha",cluster="main-cluster"} 100
# HELP rustfs_replication_pending_count Objects still waiting to be replicated (current backlog count). Unit: objects.
# TYPE rustfs_replication_pending_count gauge
rustfs_replication_pending_count{bucket="alpha",cluster="main-cluster"} 2
# HELP rustfs_replication_completed_bytes Total bytes successfully replicated since rustfs started (cumulative counter). Use rate(...[5m]) to get throughput. Unit: bytes.
# TYPE rustfs_replication_completed_bytes gauge
rustfs_replication_completed_bytes{bucket="alpha",cluster="main-cluster"} 500
# HELP rustfs_replication_completed_count Total objects successfully replicated since rustfs started (cumulative counter). Unit: objects.
# TYPE rustfs_replication_completed_count gauge
rustfs_replication_completed_count{bucket="alpha",cluster="main-cluster"} 5
# HELP rustfs_replication_failed_count Total objects that failed replication since rustfs started (cumulative counter). Use rate(...[5m]) for failure rate. Unit: objects.
# TYPE rustfs_replication_failed_count gauge
rustfs_replication_failed_count{bucket="alpha",cluster="main-cluster"} 1
# HELP rustfs_replication_bandwidth_current_bytes Instantaneous replication bandwidth reported by rustfs admin API (summed across all target ARNs). Unit: bytes/sec.
# TYPE rustfs_replication_bandwidth_current_bytes gauge
rustfs_replication_bandwidth_current_bytes{bucket="alpha",cluster="main-cluster"} 1024
# HELP rustfs_replication_queue_current_bytes Current queue depth in bytes (sampled now). Unit: bytes.
# TYPE rustfs_replication_queue_current_bytes gauge
rustfs_replication_queue_current_bytes{bucket="alpha",cluster="main-cluster"} 50
# HELP rustfs_replication_queue_last_minute_bytes Average queue depth in bytes over the last minute. Unit: bytes.
# TYPE rustfs_replication_queue_last_minute_bytes gauge
rustfs_replication_queue_last_minute_bytes{bucket="alpha",cluster="main-cluster"} 200
# HELP rustfs_replication_queue_max_bytes Maximum queue depth in bytes observed since rustfs started. Unit: bytes.
# TYPE rustfs_replication_queue_max_bytes gauge
rustfs_replication_queue_max_bytes{bucket="alpha",cluster="main-cluster"} 80
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), gatherNames()...); err != nil {
		t.Fatal(err)
	}
}

func TestCollector_UpdateHealth(t *testing.T) {
	c := NewCollector()
	reg := prometheus.NewRegistry()
	c.Register(reg)

	c.SetOverallUp(true)
	c.UpdateHealth("main-cluster", &rustfs.HealthResponse{
		Details: map[string]rustfs.HealthCheck{
			"storage": {Ready: true},
			"iam":     {Ready: true},
			"lock":    {Ready: false},
		},
	})

	if v := testutil.ToFloat64(c.Up); v != 1 {
		t.Errorf("up=%v", v)
	}
	if v := testutil.ToFloat64(c.Health.WithLabelValues("main-cluster", "storage")); v != 1 {
		t.Errorf("storage=%v", v)
	}
	if v := testutil.ToFloat64(c.Health.WithLabelValues("main-cluster", "lock")); v != 0 {
		t.Errorf("lock=%v", v)
	}
}

func TestCollector_MultiInstance(t *testing.T) {
	c := NewCollector()
	reg := prometheus.NewRegistry()
	c.Register(reg)

	// 两个 instance 同一桶
	c.UpdateReplication("main-cluster", "alpha", &rustfs.ReplicationStats{
		Stats:           map[string]rustfs.PerARNStat{"arn:1": {ReplicatedSize: 1000, ReplicatedCount: 10, Failed: rustfs.FailedM{Count: 0}, CurrentBandwidthBytesPerSec: 500}},
		ReplicaSize:     200,
		ReplicaCount:    2,
		ReplicatedSize:  1000,
		ReplicatedCount: 10,
	})
	c.UpdateReplication("target", "alpha", &rustfs.ReplicationStats{
		Stats:           map[string]rustfs.PerARNStat{"arn:1": {ReplicatedSize: 800, ReplicatedCount: 8, Failed: rustfs.FailedM{Count: 1}, CurrentBandwidthBytesPerSec: 300}},
		ReplicaSize:     100,
		ReplicaCount:    1,
		ReplicatedSize:  800,
		ReplicatedCount: 8,
	})

	if v := testutil.ToFloat64(c.CompletedBytes.WithLabelValues("main-cluster", "alpha")); v != 1000 {
		t.Errorf("source completed_bytes=%v, want 1000", v)
	}
	if v := testutil.ToFloat64(c.CompletedBytes.WithLabelValues("target", "alpha")); v != 800 {
		t.Errorf("target completed_bytes=%v, want 800", v)
	}
	if v := testutil.ToFloat64(c.FailedCount.WithLabelValues("main-cluster", "alpha")); v != 0 {
		t.Errorf("source failed_count=%v, want 0", v)
	}
	if v := testutil.ToFloat64(c.FailedCount.WithLabelValues("target", "alpha")); v != 1 {
		t.Errorf("target failed_count=%v, want 1", v)
	}
}

func gatherNames() []string {
	return []string{
		"rustfs_replication_pending_bytes",
		"rustfs_replication_pending_count",
		"rustfs_replication_completed_bytes",
		"rustfs_replication_completed_count",
		"rustfs_replication_failed_count",
		"rustfs_replication_bandwidth_current_bytes",
		"rustfs_replication_queue_current_bytes",
		"rustfs_replication_queue_last_minute_bytes",
		"rustfs_replication_queue_max_bytes",
	}
}