package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
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
	if err := validateSecurityConfig(cfg); err != nil {
		log.Fatalf("unsafe configuration: %v", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	mux := http.NewServeMux()
	server.New(cfg, st, mux, web.Handler())

	httpServer := &http.Server{Addr: cfg.Listen, Handler: publicURLHostHandler(cfg.PublicURL, mux),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}

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

func publicURLHostHandler(publicURL string, next http.Handler) http.Handler {
	u, err := url.Parse(publicURL)
	if err != nil {
		panic("publicURLHostHandler called with invalid public URL")
	}
	expected := u.Host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok\n"))
			return
		}
		if !strings.EqualFold(r.Host, expected) {
			http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Development authentication bypass is opt-in and never binds a public socket.
func validateSecurityConfig(cfg *model.Config) error {
	if cfg.InsecureDevelopment {
		host, _, err := net.SplitHostPort(cfg.Listen)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return fmt.Errorf("insecureDevelopment requires an explicit loopback listen address")
		}
		if cfg.PublicURL == "" {
			cfg.PublicURL = "http://" + cfg.Listen
		}
		u, err := url.Parse(cfg.PublicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != cfg.Listen || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("development publicUrl must match the loopback listen origin (or be omitted)")
		}
		cfg.PublicURL = strings.TrimSuffix(cfg.PublicURL, "/")
		return nil
	}
	if strings.TrimSpace(cfg.GitHub.ClientID) == "" || strings.TrimSpace(cfg.GitHub.ClientSecret) == "" {
		return fmt.Errorf("production requires GitHub clientId and clientSecret")
	}
	if len(cfg.GitHub.AllowedUsers) == 0 {
		return fmt.Errorf("production requires a non-empty GitHub allowedUsers list")
	}
	for _, user := range cfg.GitHub.AllowedUsers {
		if strings.TrimSpace(user) == "" {
			return fmt.Errorf("allowedUsers must not contain empty names")
		}
	}
	u, err := url.Parse(cfg.PublicURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("production publicUrl must be an HTTPS origin")
	}
	cfg.PublicURL = strings.TrimSuffix(cfg.PublicURL, "/")
	return nil
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
