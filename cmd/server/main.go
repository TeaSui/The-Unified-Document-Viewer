package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
	"github.com/tungnguyen/unified-document-viewer/internal/aggregator"
	"github.com/tungnguyen/unified-document-viewer/internal/config"
	"github.com/tungnguyen/unified-document-viewer/internal/documents"
	"github.com/tungnguyen/unified-document-viewer/internal/health"
	"github.com/tungnguyen/unified-document-viewer/internal/observability"
	"github.com/tungnguyen/unified-document-viewer/internal/platform/postgres"
	"github.com/tungnguyen/unified-document-viewer/internal/repository"
	"github.com/tungnguyen/unified-document-viewer/internal/upstream"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("application failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	tp, err := observability.InitTracer(ctx, cfg.OTLPEndpoint)
	if err != nil {
		slog.Warn("tracing initialization failed, continuing without tracing", "error", err)
	} else {
		defer func() { _ = tp.Shutdown(ctx) }()
	}

	mp, metricsHandler, err := observability.InitMeter()
	if err != nil {
		return fmt.Errorf("initializing metrics: %w", err)
	}
	defer func() { _ = mp.Shutdown(ctx) }()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	// Redis cache
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("parsing redis URL: %w", err)
	}
	redisClient := redis.NewClient(redisOpts)
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("connecting to redis: %w", err)
	}
	slog.Info("redis connected", "url", cfg.RedisURL)

	cacheRepo := repository.NewRedisCacheRepository(redisClient, cfg.RedisCacheTTL)

	// Kafka audit producer
	kafkaBrokers := strings.Split(cfg.KafkaBrokers, ",")
	auditRepo := repository.NewKafkaAuditRepository(kafkaBrokers, cfg.KafkaTopic)
	defer auditRepo.Close()

	// Kafka audit consumer (GORM → Postgres in background)
	gormDB, err := repository.NewGormDB(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("initializing GORM: %w", err)
	}
	auditConsumer := repository.NewAuditConsumer(kafkaBrokers, cfg.KafkaTopic, "audit-consumer", gormDB)
	defer auditConsumer.Close()
	go auditConsumer.Run(ctx)

	resiliencyCfg := upstream.ResiliencyConfig{
		Timeout:          cfg.UpstreamTimeout,
		RetryBaseDelay:   cfg.RetryBaseDelay,
		BreakerThreshold: cfg.BreakerThreshold,
	}

	salesClient := upstream.NewResilientClient(cfg.SalesMockURL, "sales", resiliencyCfg)
	serviceClient := upstream.NewResilientClient(cfg.ServiceMockURL, "service", resiliencyCfg)

	aggService := aggregator.NewService(
		[]aggregator.Source{salesClient, serviceClient},
		aggregator.WithCache(cacheRepo),
		aggregator.WithDeadline(cfg.RequestDeadline),
	)
	docsHandler := documents.NewHandler(aggService, documents.WithAudit(auditRepo))

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(observability.TracingMiddleware)
	r.Use(observability.LoggingMiddleware)
	r.Use(middleware.Recoverer)

	healthHandler := health.NewHandler(pool)
	r.Get("/healthz", healthHandler.Liveness)
	r.Get("/readyz", healthHandler.Readiness)
	r.Get("/vehicles/{vin}/documents", docsHandler.GetDocuments)
	r.Handle("/metrics", metricsHandler)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			cancel()
		}
	}()

	<-quit
	slog.Info("shutting down server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	return srv.Shutdown(shutdownCtx)
}
