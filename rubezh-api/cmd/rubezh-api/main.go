// Команда rubezh-api — API Gateway «Рубеж ИИ».
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/api"
	"github.com/rubezh-ai/rubezh-api/internal/config"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("ошибка конфигурации", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	store, err := storage.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("ошибка подключения к БД", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if err := store.Ping(ctx); err != nil {
		logger.Error("БД недоступна", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           api.NewRouter(logger),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("rubezh-api запущен", "port", cfg.HTTPPort)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("ошибка HTTP-сервера", "error", err)
		os.Exit(1)
	}
}

// healthcheck выполняет self-проверку для Docker HEALTHCHECK
// (образ distroless не содержит shell и curl).
func healthcheck() int {
	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/health")
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
