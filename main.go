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

	// Background retention pruning.
	pruneCtx, stopPrune := context.WithCancel(context.Background())
	go pruneLoop(pruneCtx, st, cfg.RetentionDays)

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

	stopPrune()
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
	return &cfg, nil
}

func pruneLoop(ctx context.Context, st *store.Store, retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if n, err := st.PruneUsages(retentionDays); err != nil {
			log.Printf("prune error: %v", err)
		} else if n > 0 {
			log.Printf("pruned %d old usage rows", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
