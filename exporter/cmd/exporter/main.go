package main

import (
	"fmt"
	"os"

	"github.com/local/rustfs-exporter/internal/config"
	// Prometheus client libraries are wired into the collector and HTTP
	// handler in subsequent tasks; imported here so they are pinned as
	// direct module dependencies in go.mod / go.sum.
	_ "github.com/prometheus/client_golang/prometheus"
	_ "github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	if _, err := config.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	// HTTP server 由后续任务接入
}
