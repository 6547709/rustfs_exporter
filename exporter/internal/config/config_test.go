package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("RUSTFS_ENDPOINT", "")
	t.Setenv("RUSTFS_ACCESS_KEY", "ak")
	t.Setenv("RUSTFS_SECRET_KEY", "sk")
	t.Setenv("RUSTFS_REGION", "")
	t.Setenv("RUSTFS_EXPORTER_LISTEN", "")
	t.Setenv("RUSTFS_EXPORTER_SCRAPE_INTERVAL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Endpoint != "http://127.0.0.1:9000" {
		t.Errorf("Endpoint default = %q, want %q", cfg.Endpoint, "http://127.0.0.1:9000")
	}
	if cfg.Region != "us-east-1" {
		t.Errorf("Region default = %q, want %q", cfg.Region, "us-east-1")
	}
	if cfg.Listen != ":9090" {
		t.Errorf("Listen default = %q, want %q", cfg.Listen, ":9090")
	}
	if cfg.ScrapeInterval != 30*time.Second {
		t.Errorf("ScrapeInterval default = %v, want %v", cfg.ScrapeInterval, 30*time.Second)
	}
}

func TestLoad_Required(t *testing.T) {
	t.Setenv("RUSTFS_ACCESS_KEY", "")
	t.Setenv("RUSTFS_SECRET_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error when creds missing")
	}
}

func TestLoad_Override(t *testing.T) {
	t.Setenv("RUSTFS_ENDPOINT", "http://rustfs.local:9000")
	t.Setenv("RUSTFS_ACCESS_KEY", "ak")
	t.Setenv("RUSTFS_SECRET_KEY", "sk")
	t.Setenv("RUSTFS_REGION", "cn-north-1")
	t.Setenv("RUSTFS_EXPORTER_LISTEN", ":9999")
	t.Setenv("RUSTFS_EXPORTER_SCRAPE_INTERVAL", "10s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Endpoint != "http://rustfs.local:9000" {
		t.Errorf("Endpoint = %q", cfg.Endpoint)
	}
	if cfg.Region != "cn-north-1" {
		t.Errorf("Region = %q", cfg.Region)
	}
	if cfg.Listen != ":9999" {
		t.Errorf("Listen = %q", cfg.Listen)
	}
	if cfg.ScrapeInterval != 10*time.Second {
		t.Errorf("ScrapeInterval = %v", cfg.ScrapeInterval)
	}
}
