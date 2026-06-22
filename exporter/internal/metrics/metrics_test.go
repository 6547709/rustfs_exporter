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

	c.UpdateReplication("alpha", &rustfs.ReplicationStats{
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
# HELP rustfs_replication_pending_bytes Bytes pending replication.
# TYPE rustfs_replication_pending_bytes gauge
rustfs_replication_pending_bytes{bucket="alpha"} 100
# HELP rustfs_replication_pending_count Objects pending replication.
# TYPE rustfs_replication_pending_count gauge
rustfs_replication_pending_count{bucket="alpha"} 2
# HELP rustfs_replication_completed_bytes Bytes replicated in total.
# TYPE rustfs_replication_completed_bytes gauge
rustfs_replication_completed_bytes{bucket="alpha"} 500
# HELP rustfs_replication_completed_count Objects replicated in total.
# TYPE rustfs_replication_completed_count gauge
rustfs_replication_completed_count{bucket="alpha"} 5
# HELP rustfs_replication_failed_count Failed objects in total.
# TYPE rustfs_replication_failed_count gauge
rustfs_replication_failed_count{bucket="alpha"} 1
# HELP rustfs_replication_bandwidth_current_bytes Current replication bandwidth.
# TYPE rustfs_replication_bandwidth_current_bytes gauge
rustfs_replication_bandwidth_current_bytes{bucket="alpha"} 1024
# HELP rustfs_replication_queue_current_bytes Current queue depth in bytes.
# TYPE rustfs_replication_queue_current_bytes gauge
rustfs_replication_queue_current_bytes{bucket="alpha"} 50
# HELP rustfs_replication_queue_last_minute_bytes Bytes enqueued in last minute.
# TYPE rustfs_replication_queue_last_minute_bytes gauge
rustfs_replication_queue_last_minute_bytes{bucket="alpha"} 200
# HELP rustfs_replication_queue_max_bytes Max queue depth since start.
# TYPE rustfs_replication_queue_max_bytes gauge
rustfs_replication_queue_max_bytes{bucket="alpha"} 80
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), gatherNames()...); err != nil {
		t.Fatal(err)
	}
}

func TestCollector_UpdateHealth(t *testing.T) {
	c := NewCollector()
	reg := prometheus.NewRegistry()
	c.Register(reg)

	c.Up.Set(1)
	c.UpdateHealth(&rustfs.HealthResponse{
		Details: map[string]rustfs.HealthCheck{
			"storage": {Ready: true},
			"iam":     {Ready: true},
			"lock":    {Ready: false},
		},
	})

	if v := testutil.ToFloat64(c.Up); v != 1 {
		t.Errorf("up=%v", v)
	}
	if v := testutil.ToFloat64(c.Health.WithLabelValues("storage")); v != 1 {
		t.Errorf("storage=%v", v)
	}
	if v := testutil.ToFloat64(c.Health.WithLabelValues("lock")); v != 0 {
		t.Errorf("lock=%v", v)
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