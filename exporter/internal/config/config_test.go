package config

import (
	"testing"
	"time"
)

func TestLoad_SingleTarget(t *testing.T) {
	t.Setenv("RUSTFS_TARGETS_JSON", "")
	t.Setenv("RUSTFS_ACCESS_KEY", "ak")
	t.Setenv("RUSTFS_SECRET_KEY", "sk")
	t.Setenv("RUSTFS_ENDPOINT", "https://example.com:9000")
	t.Setenv("RUSTFS_REGION", "us-west-2")
	t.Setenv("RUSTFS_TLS_SKIP_VERIFY", "true")
	t.Setenv("RUSTFS_EXPORTER_LISTEN", ":9999")
	t.Setenv("RUSTFS_EXPORTER_SCRAPE_INTERVAL", "10s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(cfg.Targets); got != 1 {
		t.Fatalf("Targets len=%d, want 1", got)
	}
	t0 := cfg.Targets[0]
	if t0.Name != "default" {
		t.Errorf("Name=%q, want default", t0.Name)
	}
	if t0.Endpoint != "https://example.com:9000" {
		t.Errorf("Endpoint=%q", t0.Endpoint)
	}
	if t0.Region != "us-west-2" {
		t.Errorf("Region=%q", t0.Region)
	}
	if !t0.TLSSkipVerify {
		t.Errorf("TLSSkipVerify=false")
	}
	if cfg.Listen != ":9999" {
		t.Errorf("Listen=%q", cfg.Listen)
	}
	if cfg.ScrapeInterval != 10*time.Second {
		t.Errorf("ScrapeInterval=%v", cfg.ScrapeInterval)
	}
}

func TestLoad_MultiTarget(t *testing.T) {
	t.Setenv("RUSTFS_TARGETS_JSON", `[
		{"name":"source","endpoint":"https://10.0.50.15:9000","access_key":"admin","secret_key":"s1","tls_skip_verify":true},
		{"name":"target","endpoint":"https://10.0.50.18:9000","access_key":"rustfsadmin","secret_key":"s2","region":"us-east-1"}
	]`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(cfg.Targets); got != 2 {
		t.Fatalf("Targets len=%d, want 2", got)
	}
	if cfg.Targets[0].Name != "source" || cfg.Targets[0].Endpoint != "https://10.0.50.15:9000" {
		t.Errorf("Targets[0]=%+v", cfg.Targets[0])
	}
	if cfg.Targets[1].Name != "target" || cfg.Targets[1].Region != "us-east-1" {
		t.Errorf("Targets[1]=%+v", cfg.Targets[1])
	}
}

func TestLoad_MultiTarget_AutoName(t *testing.T) {
	t.Setenv("RUSTFS_TARGETS_JSON", `[
		{"endpoint":"https://a:9000","access_key":"ak","secret_key":"sk"},
		{"endpoint":"https://b:9000","access_key":"ak","secret_key":"sk"}
	]`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Targets[0].Name != "target-0" {
		t.Errorf("auto-name[0]=%q", cfg.Targets[0].Name)
	}
	if cfg.Targets[1].Name != "target-1" {
		t.Errorf("auto-name[1]=%q", cfg.Targets[1].Name)
	}
}

func TestLoad_MultiTarget_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"empty array", `[]`},
		{"missing endpoint", `[{"access_key":"a","secret_key":"b"}]`},
		{"missing creds", `[{"endpoint":"https://x"}]`},
		{"invalid name char", `[{"name":"bad name","endpoint":"https://x","access_key":"a","secret_key":"b"}]`},
		{"malformed JSON", `[{"endpoint":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("RUSTFS_TARGETS_JSON", tt.json)
			t.Setenv("RUSTFS_ACCESS_KEY", "")
			t.Setenv("RUSTFS_SECRET_KEY", "")
			_, err := Load()
			if err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestLoad_NoCredentials(t *testing.T) {
	t.Setenv("RUSTFS_TARGETS_JSON", "")
	t.Setenv("RUSTFS_ACCESS_KEY", "")
	t.Setenv("RUSTFS_SECRET_KEY", "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when no credentials set")
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("RUSTFS_TARGETS_JSON", "")
	t.Setenv("RUSTFS_EXPORTER_LISTEN", "")
	t.Setenv("RUSTFS_EXPORTER_SCRAPE_INTERVAL", "")
	t.Setenv("RUSTFS_ENDPOINT", "")
	t.Setenv("RUSTFS_REGION", "")
	t.Setenv("RUSTFS_TLS_SKIP_VERIFY", "")
	t.Setenv("RUSTFS_ACCESS_KEY", "ak")
	t.Setenv("RUSTFS_SECRET_KEY", "sk")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":9090" {
		t.Errorf("Listen default=%q, want :9090", cfg.Listen)
	}
	if cfg.ScrapeInterval != 30*time.Second {
		t.Errorf("ScrapeInterval default=%v, want 30s", cfg.ScrapeInterval)
	}
	if cfg.Targets[0].Endpoint != "http://127.0.0.1:9000" {
		t.Errorf("Endpoint default=%q", cfg.Targets[0].Endpoint)
	}
	if cfg.Targets[0].Region != "us-east-1" {
		t.Errorf("Region default=%q", cfg.Targets[0].Region)
	}
}