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
	"github.com/rubezh-ai/rubezh-api/internal/crypto"
	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("ошибка конфигурации", "error", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel(cfg.LogLevel),
	}))

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

	providers, err := store.ListModelProviders(ctx)
	if err != nil {
		logger.Error("ошибка чтения провайдеров моделей", "error", err)
		os.Exit(1)
	}
	llmRouter := buildRouter(providers, cfg.LLMAPIKey)
	logger.Info("LLM Router инициализирован", "providers", llmRouter.Count())

	mappingCipher, err := crypto.NewCipher(cfg.MappingEncryptionKey)
	if err != nil {
		logger.Error("ошибка инициализации AES-GCM для mapping",
			"error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr: ":" + cfg.HTTPPort,
		Handler: api.NewRouter(api.Deps{
			Logger:        logger,
			Store:         store,
			AuthSecret:    cfg.AuthSecret,
			Router:        llmRouter,
			SanitizerURL:  cfg.SanitizerURL,
			MappingCipher: mappingCipher,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("rubezh-api запущен", "port", cfg.HTTPPort)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("ошибка HTTP-сервера", "error", err)
		os.Exit(1)
	}
}

// buildRouter регистрирует провайдеров LLM по конфигурации из БД.
// Отключённые (is_enabled = false) провайдеры пропускаются.
//
// MVP-ограничение: все openai_compatible-провайдеры получают общий apiKey
// (LLM_API_KEY). Отдельный ключ на каждого провайдера — зафиксированный
// техдолг (docs/PLAN.md, секция «Технический долг»).
func buildRouter(providers []storage.ModelProvider, apiKey string) *llm.Router {
	router := llm.NewRouter()
	for _, provider := range providers {
		if !provider.IsEnabled {
			continue
		}
		switch provider.Adapter {
		case "openai_compatible":
			router.Register(llm.NewOpenAIProvider(
				provider.Name, provider.Endpoint, apiKey))
		default:
			router.Register(llm.NewMockProvider(provider.Name))
		}
	}
	return router
}

// logLevel переводит строковый уровень из конфигурации в slog.Level.
func logLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// healthcheck выполняет self-проверку для Docker HEALTHCHECK
// (образ distroless не содержит shell и curl). Порт берётся из единого
// источника config.HTTPPort — тот же, что слушает HTTP-сервер.
func healthcheck() int {
	return healthcheckAt("http://127.0.0.1:" + config.HTTPPort() + "/health")
}

// healthcheckAt возвращает 0, если по адресу отвечает HTTP 200, иначе 1.
func healthcheckAt(url string) int {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
