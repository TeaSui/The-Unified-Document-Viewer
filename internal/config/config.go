package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port           string
	DatabaseURL    string
	SalesMockURL   string
	ServiceMockURL string
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

	return &Config{
		Port:           port,
		DatabaseURL:    dbURL,
		SalesMockURL:   salesURL,
		ServiceMockURL: serviceURL,
	}, nil
}
