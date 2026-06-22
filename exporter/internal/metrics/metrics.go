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
		Up:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "rustfs_up", Help: "1 if exporter last scrape succeeded."}),
		Health: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_health_ready", Help: "1 if rustfs component is ready."}, []string{"component"}),
		PendingBytes:    prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_pending_bytes",     Help: "Bytes pending replication."},         bucketLabel),
		PendingCount:    prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_pending_count",     Help: "Objects pending replication."},       bucketLabel),
		CompletedBytes:  prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_completed_bytes",   Help: "Bytes replicated in total."},         bucketLabel),
		CompletedCount:  prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_completed_count",   Help: "Objects replicated in total."},       bucketLabel),
		FailedCount:     prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_failed_count",      Help: "Failed objects in total."},  bucketLabel),
		BandwidthNow:    prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_bandwidth_current_bytes", Help: "Current replication bandwidth."}, bucketLabel),
		QueueCurrent:    prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_queue_current_bytes",     Help: "Current queue depth in bytes."},       bucketLabel),
		QueueLastMinute: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_queue_last_minute_bytes", Help: "Bytes enqueued in last minute."},  bucketLabel),
		QueueMax:        prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rustfs_replication_queue_max_bytes",         Help: "Max queue depth since start."},         bucketLabel),
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
	c.PendingBytes.WithLabelValues(bucket).Set(float64(s.ReplicaSize))
	c.PendingCount.WithLabelValues(bucket).Set(float64(s.ReplicaCount))
	c.CompletedBytes.WithLabelValues(bucket).Set(float64(s.ReplicatedSize))
	c.CompletedCount.WithLabelValues(bucket).Set(float64(s.ReplicatedCount))
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