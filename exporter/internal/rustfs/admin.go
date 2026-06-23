package rustfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/local/rustfs-exporter/internal/sigv4"
)

// ErrNoReplication 表示该桶未启用跨区域复制（目标端 rustfs 或源端未配置）。
// 调用方应跳过（不记日志、不导出 replication 指标），这是预期状态而非错误。
var ErrNoReplication = errors.New("replication not configured for bucket")

// AdminClient 复用同一组 S3 凭证调用 rustfs admin API。
type AdminClient struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	HTTP      *http.Client
}

// NewAdminClient 构造一个 Admin API 客户端。tlsOpts 用于 HTTPS 证书校验配置。
func NewAdminClient(ep, region, ak, sk string, tlsOpts TLSOptions) (*AdminClient, error) {
	httpClient, err := BuildHTTPClient(tlsOpts, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("admin client: %w", err)
	}
	return &AdminClient{
		Endpoint:  ep,
		Region:    region,
		AccessKey: ak,
		SecretKey: sk,
		HTTP:      httpClient,
	}, nil
}

// ----- Replication metrics -----
//
// JSON 来自 rustfs admin endpoint /rustfs/admin/v3/replicationmetrics，
// 序列化的是 BucketReplicationStats（crates/ecstore/src/bucket/replication/replication_state.rs）。
//
// 顶层字段（aggregated across all ARNs）：
//   replica_size / replica_count     — pending
//   replicated_size / replicated_count — completed
//   q_stat                            — InQueueMetric (4 sub-InQueueStats)
//   stats                             — HashMap<ARN, BucketReplicationStat> per-ARN
//
// 失败计数与当前带宽在源中仅在 per-ARN 的 BucketReplicationStat
// （failed.count 与 current_bandwidth_bytes_per_sec），需在 exporter 内聚合。

type ReplicationStats struct {
	Stats           map[string]PerARNStat `json:"stats"`
	ReplicaSize     int64                 `json:"replica_size"`
	ReplicaCount    int64                 `json:"replica_count"`
	ReplicatedSize  int64                 `json:"replicated_size"`
	ReplicatedCount int64                 `json:"replicated_count"`
	QStat           QStat                 `json:"q_stat"`
}

type QStat struct {
	Current    QueueValue `json:"curr"`
	Average    QueueValue `json:"avg"`
	Max        QueueValue `json:"max"`
	LastMinute QueueValue `json:"last_minute"`
}

type QueueValue struct {
	Bytes int64 `json:"bytes"`
	Count int64 `json:"count"`
}

type PerARNStat struct {
	ReplicatedSize              int64   `json:"replicated_size"`
	ReplicatedCount             int64   `json:"replicated_count"`
	Failed                      FailedM `json:"failed"`
	BandwidthLimitBytesPerSec   int64   `json:"bandwidth_limit_bytes_per_sec"`
	CurrentBandwidthBytesPerSec float64 `json:"current_bandwidth_bytes_per_sec"`
}

type FailedM struct {
	Count int64 `json:"count"`
	Size  int64 `json:"size"`
}

// TotalFailedCount 聚合所有 ARN 的 failed.count。
func (r *ReplicationStats) TotalFailedCount() int64 {
	var total int64
	for _, s := range r.Stats {
		total += s.Failed.Count
	}
	return total
}

// TotalBandwidthNow 聚合所有 ARN 的 current_bandwidth_bytes_per_sec。
func (r *ReplicationStats) TotalBandwidthNow() float64 {
	var total float64
	for _, s := range r.Stats {
		total += s.CurrentBandwidthBytesPerSec
	}
	return total
}

// ----- Health -----

type HealthResponse struct {
	Status  string                 `json:"status"`
	Ready   bool                   `json:"ready"`
	Details map[string]HealthCheck `json:"details"`
}

type HealthCheck struct {
	Status string `json:"status"`
	Ready  bool   `json:"ready"`
}

// ----- 实际网络请求（先写失败测试，下一步实现） -----

func (c *AdminClient) ReplicationMetrics(ctx context.Context, bucket string) (*ReplicationStats, error) {
	path := "/rustfs/admin/v3/replicationmetrics"
	query := "bucket=" + bucket
	url := c.Endpoint + path + "?" + query

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	auth, date, contentSHA := sigv4.Sign("GET", req.URL.Host, path, query, c.Region, "s3", nil, c.AccessKey, c.SecretKey)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Amz-Date", date)
	req.Header.Set("X-Amz-Content-Sha256", contentSHA)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("replicationmetrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// 目标端 rustfs 或源端未配置复制：admin 返回 404。
		// 调用方应跳过——这是正常状态，不算错误。
		return &ReplicationStats{Stats: map[string]PerARNStat{}}, ErrNoReplication
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	var out ReplicationStats
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("replicationmetrics decode: %w", err)
	}
	if out.Stats == nil {
		out.Stats = map[string]PerARNStat{}
	}
	return &out, nil
}

func (c *AdminClient) Health(ctx context.Context) (*HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint+"/health/ready", nil)
	if err != nil {
		return nil, err
	}
	// /health/ready 是公共端点，不签名
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("health: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health status %d: %s", resp.StatusCode, string(body))
	}
	var out HealthResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("health decode: %w", err)
	}
	return &out, nil
}
