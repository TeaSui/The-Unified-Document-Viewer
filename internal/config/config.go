package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port           string
	DatabaseURL    string
	SalesMockURL   string
	ServiceMockURL string

	UpstreamTimeout  time.Duration
	RetryBaseDelay   time.Duration
	RequestDeadline  time.Duration
	BreakerThreshold uint32
	OTLPEndpoint     string

	RedisURL      string
	RedisCacheTTL time.Duration
	KafkaBrokers  string
	KafkaTopic    string
}

func Load() (*Config, error) {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}

	salesURL := os.Getenv("SALES_MOCK_URL")
	if salesURL == "" {
		salesURL = "http://localhost:9001"
	}

	serviceURL := os.Getenv("SERVICE_MOCK_URL")
	if serviceURL == "" {
		serviceURL = "http://localhost:9002"
	}

	upstreamTimeout := parseDuration("UPSTREAM_TIMEOUT_MS", 800)
	retryBaseDelay := parseDuration("RETRY_BASE_DELAY_MS", 50)
	requestDeadline := parseDuration("REQUEST_DEADLINE_MS", 1500)
	breakerThreshold := parseUint32("BREAKER_THRESHOLD", 5)

	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "localhost:4317"
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	redisCacheTTL := parseDuration("REDIS_CACHE_TTL_MS", 3600000)

	kafkaBrokers := os.Getenv("KAFKA_BROKERS")
	if kafkaBrokers == "" {
		kafkaBrokers = "localhost:9092"
	}

	kafkaTopic := os.Getenv("KAFKA_AUDIT_TOPIC")
	if kafkaTopic == "" {
		kafkaTopic = "audit-requests"
	}

	return &Config{
		Port:             port,
		DatabaseURL:      dbURL,
		SalesMockURL:     salesURL,
		ServiceMockURL:   serviceURL,
		UpstreamTimeout:  upstreamTimeout,
		RetryBaseDelay:   retryBaseDelay,
		RequestDeadline:  requestDeadline,
		BreakerThreshold: breakerThreshold,
		OTLPEndpoint:     otlpEndpoint,
		RedisURL:         redisURL,
		RedisCacheTTL:    redisCacheTTL,
		KafkaBrokers:     kafkaBrokers,
		KafkaTopic:       kafkaTopic,
	}, nil
}

func parseDuration(envKey string, defaultMs int) time.Duration {
	val := os.Getenv(envKey)
	if val == "" {
		return time.Duration(defaultMs) * time.Millisecond
	}
	ms, err := strconv.Atoi(val)
	if err != nil {
		return time.Duration(defaultMs) * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

func parseUint32(envKey string, defaultVal uint32) uint32 {
	val := os.Getenv(envKey)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.ParseUint(val, 10, 32)
	if err != nil {
		return defaultVal
	}
	return uint32(n)
}
