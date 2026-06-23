package collector

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/local/rustfs-exporter/internal/metrics"
	"github.com/local/rustfs-exporter/internal/rustfs"
)

type ScrapeWorker struct {
	S3       *rustfs.S3Client
	Admin    *rustfs.AdminClient
	Metrics  *metrics.Collector
	Interval time.Duration
}

func NewScrapeWorker(s3 *rustfs.S3Client, adm *rustfs.AdminClient, m *metrics.Collector, interval time.Duration) *ScrapeWorker {
	return &ScrapeWorker{S3: s3, Admin: adm, Metrics: m, Interval: interval}
}

func (w *ScrapeWorker) Run(ctx context.Context) {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	w.cycle(ctx) // 立即跑一次
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.cycle(ctx)
		}
	}
}

func (w *ScrapeWorker) cycle(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	buckets, err := w.S3.ListBuckets(ctx)
	if err != nil {
		// 仅在非 ctx 取消时把 up 置 0；ctx 超时属于正常停机。
		if ctx.Err() == nil {
			log.Printf("list buckets: %v", err)
			w.Metrics.Up.Set(0)
		}
		return
	}
	w.Metrics.Up.Set(1)
	for _, b := range buckets {
		if ctx.Err() != nil {
			return
		}
		stats, err := w.Admin.ReplicationMetrics(ctx, b)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// 目标端 / 未配置复制 = 正常状态，沉默跳过。
			if errors.Is(err, rustfs.ErrNoReplication) {
				continue
			}
			log.Printf("replication %s: %v", b, err)
			continue
		}
		w.Metrics.UpdateReplication(b, stats)
	}
	if ctx.Err() != nil {
		return
	}
	if h, err := w.Admin.Health(ctx); err == nil {
		w.Metrics.UpdateHealth(h)
	} else {
		log.Printf("health: %v", err)
	}
}