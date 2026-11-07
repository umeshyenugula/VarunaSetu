package main

import (
	"context"
	"database/sql"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"hydroledger-prototype/hydroledger/verifier"
	"hydroledger-prototype/hydroledger/verifier/identity"
	"hydroledger-prototype/service"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "perform local HTTP healthcheck and exit")
	flag.Parse()
	addr := envOr("HTTP_ADDR", ":8080")
	if *healthcheck {
		c := &http.Client{Timeout: 2 * time.Second}
		resp, err := c.Get("http://127.0.0.1" + addr + "/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = resp.Body.Close()
		os.Exit(0)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		logger.Error("open db failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		logger.Error("db ping failed (run migrations first?)", "error", err)
		os.Exit(1)
	}

	v, err := verifier.New(verifier.DefaultConfig())
	if err != nil {
		logger.Error("verifier init failed", "error", err)
		os.Exit(1)
	}
	repo := &service.Repository{DB: db}
	srv, err := service.New(service.Config{
		Verifier:        v,
		KeyStore:        repo,
		Revoked:         repo,
		IdentityConfig:  identity.DefaultConfig(),
		Repository:      repo,
		Logger:          logger,
		CORSAllowOrigin: os.Getenv("CORS_ALLOW_ORIGIN"),
	})
	if err != nil {
		logger.Error("service init failed", "error", err)
		os.Exit(1)
	}
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv.StartExpiryWorker(runCtx)
	defer srv.Close()

	httpSrv := &http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("http server starting", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()
	<-runCtx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	logger.Info("shutdown complete")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
