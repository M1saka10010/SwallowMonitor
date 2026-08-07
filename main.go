package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/server"
	"github.com/M1saka10010/SwallowMonitor/store"
	"github.com/M1saka10010/SwallowMonitor/web"
	"gopkg.in/yaml.v3"
)

func main() {
	configPath := flag.String("c", "", "path to YAML config file")
	flag.Parse()

	if *configPath == "" {
		log.Fatal("missing config file: use -c <config.yaml>")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	mux := http.NewServeMux()
	server.New(cfg, st, mux, web.Handler())

	httpServer := &http.Server{Addr: cfg.Listen, Handler: mux}

	// Background maintenance: downsample aggregation + retention pruning.
	maintCtx, stopMaint := context.WithCancel(context.Background())
	go maintenanceLoop(maintCtx, st, *cfg.RetentionDays)

	go func() {
		log.Printf("SwallowMonitor listening on %s", cfg.Listen)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")

	stopMaint()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown error: %v", err)
	}
}

func loadConfig(path string) (*model.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg model.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "./swallow.db"
	}
	if cfg.OfflineTimeout <= 0 {
		cfg.OfflineTimeout = 90
	}
	// 未配置 retentionDays 时默认 7 天，避免"0 = 永不清理"导致数据库无限增长。
	if cfg.RetentionDays == nil {
		days := 7
		cfg.RetentionDays = &days
	}
	return &cfg, nil
}

// maintenanceLoop runs every 5 minutes: it rolls raw usage rows up into the
// downsampled tables first (so pruning never loses history), then prunes the
// raw (fixed 1 day) and downsampled tiers, and reclaims disk space when the
// database is fragmented.
func maintenanceLoop(ctx context.Context, st *store.Store, retentionDays int) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		if err := st.AggregateUsage(time.Now()); err != nil {
			log.Printf("aggregate error: %v", err)
		}
		if n, err := st.PruneUsages(1); err != nil {
			log.Printf("prune error: %v", err)
		} else if n > 0 {
			log.Printf("pruned %d raw usage rows", n)
		}
		if n, err := st.PruneDownsampled(retentionDays); err != nil {
			log.Printf("prune downsampled error: %v", err)
		} else if n > 0 {
			log.Printf("pruned %d downsampled rows", n)
		}
		if vacuumed, err := st.VacuumIfFragmented(); err != nil {
			log.Printf("vacuum error: %v", err)
		} else if vacuumed {
			log.Printf("vacuumed database (free pages reclaimed)")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
