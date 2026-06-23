package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Endpoint       string
	AccessKey      string
	SecretKey      string
	Region         string
	Listen         string
	ScrapeInterval time.Duration
	CACertPath     string
	TLSSkipVerify  bool
}

func Load() (Config, error) {
	ak := os.Getenv("RUSTFS_ACCESS_KEY")
	sk := os.Getenv("RUSTFS_SECRET_KEY")
	if ak == "" || sk == "" {
		return Config{}, errors.New("RUSTFS_ACCESS_KEY and RUSTFS_SECRET_KEY are required")
	}

	interval := 30 * time.Second
	if v := os.Getenv("RUSTFS_EXPORTER_SCRAPE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid RUSTFS_EXPORTER_SCRAPE_INTERVAL: %w", err)
		}
		interval = d
	}

	endpoint := os.Getenv("RUSTFS_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9000"
	}

	region := os.Getenv("RUSTFS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	listen := os.Getenv("RUSTFS_EXPORTER_LISTEN")
	if listen == "" {
		listen = ":9090"
	}

	caCert := os.Getenv("RUSTFS_CA_CERT")

	skipVerify := false
	if v := os.Getenv("RUSTFS_TLS_SKIP_VERIFY"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid RUSTFS_TLS_SKIP_VERIFY %q: %w", v, err)
		}
		skipVerify = b
	}

	return Config{
		Endpoint:       endpoint,
		AccessKey:      ak,
		SecretKey:      sk,
		Region:         region,
		Listen:         listen,
		ScrapeInterval: interval,
		CACertPath:     caCert,
		TLSSkipVerify:  skipVerify,
	}, nil
}