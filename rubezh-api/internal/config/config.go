// Package config загружает конфигурацию сервиса rubezh-api из окружения.
package config

import (
	"fmt"
	"os"
)

// Config — конфигурация сервиса rubezh-api.
type Config struct {
	HTTPPort     string
	LogLevel     string
	DatabaseURL  string
	AuthSecret   string
	SanitizerURL string
}

// Load читает конфигурацию из переменных окружения, подставляя значения по
// умолчанию. Возвращает ошибку, если обязательные параметры не заданы.
func Load() (Config, error) {
	cfg := Config{
		HTTPPort:     getEnv("API_PORT", "8080"),
		LogLevel:     getEnv("API_LOG_LEVEL", "info"),
		DatabaseURL:  databaseURL(),
		AuthSecret:   os.Getenv("AUTH_DEV_TOKEN_SECRET"),
		SanitizerURL: getEnv("SANITIZER_URL", "http://rubezh-sanitizer:8001"),
	}
	if cfg.AuthSecret == "" {
		return Config{}, fmt.Errorf("config: переменная AUTH_DEV_TOKEN_SECRET обязательна")
	}
	return cfg, nil
}

// databaseURL возвращает DSN: из DATABASE_URL либо собранный из POSTGRES_*.
func databaseURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		getEnv("POSTGRES_USER", "rubezh"),
		getEnv("POSTGRES_PASSWORD", "rubezh"),
		getEnv("POSTGRES_HOST", "postgres"),
		getEnv("POSTGRES_PORT", "5432"),
		getEnv("POSTGRES_DB", "rubezh"),
	)
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
