package metrics

import (
	"github.com/local/rustfs-exporter/internal/rustfs"
	"github.com/prometheus/client_golang/prometheus"
)

type Collector struct {
	Up              prometheus.Gauge
	Health          *prometheus.GaugeVec
	PendingBytes    *prometheus.GaugeVec
	PendingCount    *prometheus.GaugeVec
	CompletedBytes  *prometheus.GaugeVec
	CompletedCount  *prometheus.GaugeVec
	FailedCount     *prometheus.GaugeVec
	BandwidthNow    *prometheus.GaugeVec
	QueueCurrent    *prometheus.GaugeVec
	QueueLastMinute *prometheus.GaugeVec
	QueueMax        *prometheus.GaugeVec
}

func NewCollector() *Collector {
	bucketLabel := []string{"bucket"}
	return &Collector{
		Up:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "rustfs_up", Help: "1 if the exporter's last scrape of rustfs (S3 ListBuckets) succeeded, else 0."}),
		Health: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_health_ready", Help: "1 if the rustfs component is ready (storage / iam / lock)."}, []string{"component"}),

		// Replication metrics. All byte/count metrics are absolute current values
		// (gauges) — Prometheus convention reserves `_total` for cumulative counters.
		// "completed" = successfully replicated. Counters only go up; gauges track
		// instantaneous state.
		PendingBytes:    prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_pending_bytes",     Help: "Bytes still waiting to be replicated (current backlog size). Unit: bytes."},         bucketLabel),
		PendingCount:    prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_pending_count",     Help: "Objects still waiting to be replicated (current backlog count). Unit: objects."},       bucketLabel),
		CompletedBytes:  prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_completed_bytes",   Help: "Total bytes successfully replicated since rustfs started (cumulative counter). Use rate(...[5m]) to get throughput. Unit: bytes."}, bucketLabel),
		CompletedCount:  prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_completed_count",   Help: "Total objects successfully replicated since rustfs started (cumulative counter). Unit: objects."}, bucketLabel),
		FailedCount:     prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_failed_count",      Help: "Total objects that failed replication since rustfs started (cumulative counter). Use rate(...[5m]) for failure rate. Unit: objects."}, bucketLabel),
		BandwidthNow:    prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_bandwidth_current_bytes", Help: "Instantaneous replication bandwidth reported by rustfs admin API (summed across all target ARNs). Unit: bytes/sec."}, bucketLabel),
		QueueCurrent:    prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_queue_current_bytes",     Help: "Current queue depth in bytes (sampled now). Unit: bytes."},       bucketLabel),
		QueueLastMinute: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_queue_last_minute_bytes", Help: "Average queue depth in bytes over the last minute. Unit: bytes."},  bucketLabel),
		QueueMax:        prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_queue_max_bytes",         Help: "Maximum queue depth in bytes observed since rustfs started. Unit: bytes."},         bucketLabel),
	}
}

func (c *Collector) Register(reg prometheus.Registerer) {
	reg.MustRegister(
		c.Up, c.Health,
		c.PendingBytes, c.PendingCount,
		c.CompletedBytes, c.CompletedCount,
		c.FailedCount, c.BandwidthNow,
		c.QueueCurrent, c.QueueLastMinute, c.QueueMax,
	)
}

func (c *Collector) UpdateReplication(bucket string, s *rustfs.ReplicationStats) {
	if s == nil {
		return
	}
	// rustfs 在顶层 aggregated 字段（replica_size/replicated_size 等）有时为 0，
	// 真实数据在 per-ARN 的 stats map 里。优先聚合 per-ARN，回退到顶层字段。
	var perARNSumReplicatedSize, perARNSumReplicatedCount int64
	for _, arn := range s.Stats {
		perARNSumReplicatedSize += arn.ReplicatedSize
		perARNSumReplicatedCount += arn.ReplicatedCount
	}
	completedBytes := perARNSumReplicatedSize
	if completedBytes == 0 {
		completedBytes = s.ReplicatedSize
	}
	completedCount := perARNSumReplicatedCount
	if completedCount == 0 {
		completedCount = s.ReplicatedCount
	}

	c.PendingBytes.WithLabelValues(bucket).Set(float64(s.ReplicaSize))
	c.PendingCount.WithLabelValues(bucket).Set(float64(s.ReplicaCount))
	c.CompletedBytes.WithLabelValues(bucket).Set(float64(completedBytes))
	c.CompletedCount.WithLabelValues(bucket).Set(float64(completedCount))
	c.FailedCount.WithLabelValues(bucket).Set(float64(s.TotalFailedCount()))
	c.BandwidthNow.WithLabelValues(bucket).Set(s.TotalBandwidthNow())
	c.QueueCurrent.WithLabelValues(bucket).Set(float64(s.QStat.Current.Bytes))
	c.QueueLastMinute.WithLabelValues(bucket).Set(float64(s.QStat.LastMinute.Bytes))
	c.QueueMax.WithLabelValues(bucket).Set(float64(s.QStat.Max.Bytes))
}

func (c *Collector) UpdateHealth(h *rustfs.HealthResponse) {
	if h == nil {
		return
	}
	for _, comp := range []string{"storage", "iam", "lock"} {
		v := 0.0
		if check, ok := h.Details[comp]; ok && check.Ready {
			v = 1
		}
		c.Health.WithLabelValues(comp).Set(v)
	}
}