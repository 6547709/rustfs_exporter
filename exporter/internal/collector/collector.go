package collector

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/local/rustfs-exporter/internal/config"
	"github.com/local/rustfs-exporter/internal/metrics"
	"github.com/local/rustfs-exporter/internal/rustfs"
)

// TargetClient 是一个 rustfs 目标的 S3 + admin 客户端对。
type TargetClient struct {
	Name  string
	S3    *rustfs.S3Client
	Admin *rustfs.AdminClient
}

// ScrapeWorker 周期性地从所有 rustfs 目标抓取指标。
type ScrapeWorker struct {
	Targets  []TargetClient
	Metrics  *metrics.Collector
	Interval time.Duration
}

func NewScrapeWorker(targets []config.Target, m *metrics.Collector, interval time.Duration) (*ScrapeWorker, error) {
	tcs := make([]TargetClient, 0, len(targets))
	for _, t := range targets {
		tlsOpts := rustfs.TLSOptions{
			CACertPath: t.CACertPath,
			SkipVerify: t.TLSSkipVerify,
		}
		s3, err := rustfs.NewS3Client(t.Endpoint, t.Region, t.AccessKey, t.SecretKey, tlsOpts)
		if err != nil {
			return nil, err
		}
		adm, err := rustfs.NewAdminClient(t.Endpoint, t.Region, t.AccessKey, t.SecretKey, tlsOpts)
		if err != nil {
			return nil, err
		}
		tcs = append(tcs, TargetClient{Name: t.Name, S3: s3, Admin: adm})
	}
	return &ScrapeWorker{
		Targets:  tcs,
		Metrics:  m,
		Interval: interval,
	}, nil
}

func (w *ScrapeWorker) Run(ctx context.Context) {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	w.cycle(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.cycle(ctx)
		}
	}
}

// cycle 对每个 target 跑一次完整抓取。
// 任一 target 成功 ListBuckets → 整体 Up=1；全部失败 → Up=0。
func (w *ScrapeWorker) cycle(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	anyUp := false
	for _, t := range w.Targets {
		if ctx.Err() != nil {
			return
		}
		if w.cycleTarget(ctx, t) {
			anyUp = true
		}
	}
	if ctx.Err() == nil {
		w.Metrics.SetOverallUp(anyUp)
	}
}

// cycleTarget 抓单个 target，返回 true 表示 ListBuckets 成功。
func (w *ScrapeWorker) cycleTarget(ctx context.Context, t TargetClient) bool {
	buckets, err := t.S3.ListBuckets(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return false
		}
		log.Printf("list buckets [%s] %s: %v", t.Name, t.S3.Endpoint, err)
		return false
	}
	for _, b := range buckets {
		if ctx.Err() != nil {
			return true
		}
		stats, err := t.Admin.ReplicationMetrics(ctx, b)
		if err != nil {
			if ctx.Err() != nil {
				return true
			}
			if errors.Is(err, rustfs.ErrNoReplication) {
				continue
			}
			log.Printf("replication [%s/%s]: %v", t.Name, b, err)
			continue
		}
		w.Metrics.UpdateReplication(t.Name, b, stats)
	}
	if ctx.Err() != nil {
		return true
	}
	if h, err := t.Admin.Health(ctx); err == nil {
		w.Metrics.UpdateHealth(t.Name, h)
	} else {
		if ctx.Err() == nil {
			log.Printf("health [%s]: %v", t.Name, err)
		}
	}
	return true
}