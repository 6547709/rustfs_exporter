package rustfs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminClient_ReplicationMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Query().Get("bucket") != "alpha" {
			t.Errorf("bad request: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("missing auth")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
  "stats": {
    "arn:1": {"replicated_size": 1024, "replicated_count": 5,
              "failed": {"count": 1, "size": 100},
              "bandwidth_limit_bytes_per_sec": 0,
              "current_bandwidth_bytes_per_sec": 2048.0},
    "arn:2": {"replicated_size": 0, "replicated_count": 0,
              "failed": {"count": 2, "size": 50},
              "bandwidth_limit_bytes_per_sec": 0,
              "current_bandwidth_bytes_per_sec": 1024.5}
  },
  "replica_size": 999,
  "replica_count": 3,
  "replicated_size": 1024,
  "replicated_count": 5,
  "q_stat": {
    "curr":        {"bytes": 100, "count": 2},
    "avg":         {"bytes": 200, "count": 4},
    "max":         {"bytes": 500, "count": 6},
    "last_minute": {"bytes": 256, "count": 3}
  }
}`))
	}))
	defer srv.Close()

	c, err := NewAdminClient(srv.URL, "us-east-1", "ak", "sk", TLSOptions{})
	if err != nil {
		t.Fatalf("NewAdminClient: %v", err)
	}
	got, err := c.ReplicationMetrics(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("ReplicationMetrics: %v", err)
	}
	if got.ReplicaSize != 999 {
		t.Errorf("ReplicaSize=%d", got.ReplicaSize)
	}
	if got.QStat.Current.Bytes != 100 {
		t.Errorf("QStat.Current.Bytes=%d", got.QStat.Current.Bytes)
	}
	if got.QStat.LastMinute.Bytes != 256 {
		t.Errorf("QStat.LastMinute.Bytes=%d", got.QStat.LastMinute.Bytes)
	}
	if got.QStat.Max.Bytes != 500 {
		t.Errorf("QStat.Max.Bytes=%d", got.QStat.Max.Bytes)
	}
	if fc := got.TotalFailedCount(); fc != 3 {
		t.Errorf("TotalFailedCount=%d, want 3", fc)
	}
	if bw := got.TotalBandwidthNow(); bw != 3072.5 {
		t.Errorf("TotalBandwidthNow=%v, want 3072.5", bw)
	}
}

func TestAdminClient_Health(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/ready" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
  "status":"ok","ready":true,
  "details":{
    "storage":{"status":"ok","ready":true},
    "iam":{"status":"ok","ready":true},
    "lock":{"status":"degraded","ready":false},
    "kms":{"status":"ok","ready":true}
  }
}`))
	}))
	defer srv.Close()

	c, err := NewAdminClient(srv.URL, "us-east-1", "ak", "sk", TLSOptions{})
	if err != nil {
		t.Fatalf("NewAdminClient: %v", err)
	}
	got, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !got.Details["storage"].Ready {
		t.Error("storage.ready=false")
	}
	if !got.Details["iam"].Ready {
		t.Error("iam.ready=false")
	}
	if got.Details["lock"].Ready {
		t.Error("lock.ready=true, want false")
	}
}
