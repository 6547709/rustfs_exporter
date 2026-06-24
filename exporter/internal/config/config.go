package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Target 代表一个 rustfs 实例。exporter 会为每个 Target 创建一个独立的 S3+admin
// 客户端对，所有指标都带 instance=<Name> 标签。
type Target struct {
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key"`
	Region        string `json:"region,omitempty"`
	TLSSkipVerify bool   `json:"tls_skip_verify,omitempty"`
	CACertPath    string `json:"ca_cert,omitempty"` // 留空 = 用系统默认
}

type Config struct {
	Targets        []Target
	Listen         string
	ScrapeInterval time.Duration
}

func Load() (Config, error) {
	listen := os.Getenv("RUSTFS_EXPORTER_LISTEN")
	if listen == "" {
		listen = ":9090"
	}

	interval := 30 * time.Second
	if v := os.Getenv("RUSTFS_EXPORTER_SCRAPE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid RUSTFS_EXPORTER_SCRAPE_INTERVAL: %w", err)
		}
		interval = d
	}

	targets, err := loadTargets()
	if err != nil {
		return Config{}, err
	}

	return Config{
		Targets:        targets,
		Listen:         listen,
		ScrapeInterval: interval,
	}, nil
}

// loadTargets 优先用 RUSTFS_TARGETS_JSON（JSON 数组），否则从旧单目标 env 构建。
// 单目标模式下 Target.Name 默认为 "default"。
func loadTargets() ([]Target, error) {
	if raw := os.Getenv("RUSTFS_TARGETS_JSON"); raw != "" {
		var ts []Target
		if err := json.Unmarshal([]byte(raw), &ts); err != nil {
			return nil, fmt.Errorf("invalid RUSTFS_TARGETS_JSON: %w", err)
		}
		if len(ts) == 0 {
			return nil, errors.New("RUSTFS_TARGETS_JSON is empty (at least one target required)")
		}
		for i := range ts {
			if err := validateTarget(&ts[i]); err != nil {
				return nil, fmt.Errorf("RUSTFS_TARGETS_JSON[%d]: %w", i, err)
			}
			if ts[i].Name == "" {
				ts[i].Name = fmt.Sprintf("target-%d", i)
			}
		}
		return ts, nil
	}

	// 向后兼容：单目标模式
	ak := os.Getenv("RUSTFS_ACCESS_KEY")
	sk := os.Getenv("RUSTFS_SECRET_KEY")
	if ak == "" || sk == "" {
		return nil, errors.New("either RUSTFS_TARGETS_JSON or RUSTFS_ACCESS_KEY+RUSTFS_SECRET_KEY must be set")
	}

	endpoint := os.Getenv("RUSTFS_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9000"
	}
	region := os.Getenv("RUSTFS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	skipVerify := false
	if v := os.Getenv("RUSTFS_TLS_SKIP_VERIFY"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid RUSTFS_TLS_SKIP_VERIFY %q: %w", v, err)
		}
		skipVerify = b
	}
	t := Target{
		Name:          "default",
		Endpoint:      endpoint,
		AccessKey:     ak,
		SecretKey:     sk,
		Region:        region,
		TLSSkipVerify: skipVerify,
		CACertPath:    os.Getenv("RUSTFS_CA_CERT"),
	}
	return []Target{t}, nil
}

func validateTarget(t *Target) error {
	if t.Endpoint == "" {
		return errors.New("endpoint is required")
	}
	if t.AccessKey == "" || t.SecretKey == "" {
		return errors.New("access_key and secret_key are required")
	}
	if t.Region == "" {
		t.Region = "us-east-1"
	}
	// Names are used as Prometheus labels — restrict to safe chars
	for _, r := range t.Name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return fmt.Errorf("name %q contains invalid char %q (use [a-zA-Z0-9-_])", t.Name, r)
		}
	}
	return nil
}