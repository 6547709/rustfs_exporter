package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/local/rustfs-exporter/internal/collector"
	"github.com/local/rustfs-exporter/internal/config"
	"github.com/local/rustfs-exporter/internal/metrics"
	"github.com/local/rustfs-exporter/internal/rustfs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	m := metrics.NewCollector()
	reg := prometheus.NewRegistry()
	m.Register(reg)

	s3 := rustfs.NewS3Client(cfg.Endpoint, cfg.Region, cfg.AccessKey, cfg.SecretKey)
	adm := rustfs.NewAdminClient(cfg.Endpoint, cfg.Region, cfg.AccessKey, cfg.SecretKey)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := collector.NewScrapeWorker(s3, adm, m, cfg.ScrapeInterval)
	go w.Run(ctx)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: cfg.Listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("rustfs-exporter listening on %s", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
	cancel()
	_ = srv.Shutdown(context.Background())
}