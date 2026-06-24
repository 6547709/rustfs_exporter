package metrics

import (
	"github.com/local/rustfs-exporter/internal/rustfs"
	"github.com/prometheus/client_golang/prometheus"
)

// Collector 持有所有 rustfs_* 指标向量。所有指标（除 rustfs_up 外）都带
// instance 标签 — 多 rustfs 部署时用来区分源/目标。
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
	// 每个 rustfs 实例的指标都标 instance=<Name>
	bl := []string{"cluster", "bucket"}
	cl := []string{"cluster", "component"}

	return &Collector{
		Up: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "rustfs_up",
			Help: "1 if the exporter's last scrape of any rustfs target (S3 ListBuckets) succeeded, else 0.",
		}),
		Health: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rustfs_health_ready",
			Help: "1 if the rustfs component (storage / iam / lock) is ready.",
		}, cl),

		// Replication metrics — byte/count metrics are absolute current values
		// (gauges). "completed" = successfully replicated. Counter behavior:
		// cumulative counters only go up; rates should use PromQL rate().
		PendingBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rustfs_replication_pending_bytes",
			Help: "Bytes still waiting to be replicated (current backlog size). Unit: bytes.",
		}, bl),
		PendingCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rustfs_replication_pending_count",
			Help: "Objects still waiting to be replicated (current backlog count). Unit: objects.",
		}, bl),
		CompletedBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rustfs_replication_completed_bytes",
			Help: "Total bytes successfully replicated since rustfs started (cumulative counter). Use rate(...[5m]) to get throughput. Unit: bytes.",
		}, bl),
		CompletedCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rustfs_replication_completed_count",
			Help: "Total objects successfully replicated since rustfs started (cumulative counter). Unit: objects.",
		}, bl),
		FailedCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rustfs_replication_failed_count",
			Help: "Total objects that failed replication since rustfs started (cumulative counter). Use rate(...[5m]) for failure rate. Unit: objects.",
		}, bl),
		BandwidthNow: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rustfs_replication_bandwidth_current_bytes",
			Help: "Instantaneous replication bandwidth reported by rustfs admin API (summed across all target ARNs). Unit: bytes/sec.",
		}, bl),
		QueueCurrent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rustfs_replication_queue_current_bytes",
			Help: "Current queue depth in bytes (sampled now). Unit: bytes.",
		}, bl),
		QueueLastMinute: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rustfs_replication_queue_last_minute_bytes",
			Help: "Average queue depth in bytes over the last minute. Unit: bytes.",
		}, bl),
		QueueMax: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rustfs_replication_queue_max_bytes",
			Help: "Maximum queue depth in bytes observed since rustfs started. Unit: bytes.",
		}, bl),
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

// SetClusterUp 标记某个 cluster 的整体可达性。任一 cluster 失败则整个 Up=0。
func (c *Collector) SetClusterUp(cluster string, ok bool) {
	// Up 是全局 gauge。仍用单一值反映整体健康 — 任一实例失败即 0。
	// 单值简化 dashboard 显示，避免 false alarm（多实例时常见的是某个实例挂
	// 了一瞬，整体抖动）。
	_ = cluster
	_ = ok
	// 实际整体逻辑放在 Collector.SetOverallUp
}

// SetOverallUp 标记整个 exporter 健康（任何 cluster 至少一个通）。
func (c *Collector) SetOverallUp(ok bool) {
	if ok {
		c.Up.Set(1)
	} else {
		c.Up.Set(0)
	}
}

// UpdateReplication 更新某个 cluster/bucket 的复制指标。
func (c *Collector) UpdateReplication(cluster, bucket string, s *rustfs.ReplicationStats) {
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

	labels := []string{cluster, bucket}
	c.PendingBytes.WithLabelValues(labels...).Set(float64(s.ReplicaSize))
	c.PendingCount.WithLabelValues(labels...).Set(float64(s.ReplicaCount))
	c.CompletedBytes.WithLabelValues(labels...).Set(float64(completedBytes))
	c.CompletedCount.WithLabelValues(labels...).Set(float64(completedCount))
	c.FailedCount.WithLabelValues(labels...).Set(float64(s.TotalFailedCount()))
	c.BandwidthNow.WithLabelValues(labels...).Set(s.TotalBandwidthNow())
	c.QueueCurrent.WithLabelValues(labels...).Set(float64(s.QStat.Current.Bytes))
	c.QueueLastMinute.WithLabelValues(labels...).Set(float64(s.QStat.LastMinute.Bytes))
	c.QueueMax.WithLabelValues(labels...).Set(float64(s.QStat.Max.Bytes))
}

func (c *Collector) UpdateHealth(cluster string, h *rustfs.HealthResponse) {
	if h == nil {
		return
	}
	for _, comp := range []string{"storage", "iam", "lock"} {
		v := 0.0
		if check, ok := h.Details[comp]; ok && check.Ready {
			v = 1
		}
		c.Health.WithLabelValues(cluster, comp).Set(v)
	}
}