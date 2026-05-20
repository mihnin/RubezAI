// Команда rubezh-api — API Gateway «Рубеж ИИ».
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

	mappingCipher, err := crypto.NewCipher(cfg.MappingEncryptionKey)
	if err != nil {
		logger.Error("ошибка инициализации AES-GCM для mapping",
			"error", err)
		os.Exit(1)
	}

	providers, err := store.ListModelProviders(ctx)
	if err != nil {
		logger.Error("ошибка чтения провайдеров моделей", "error", err)
		os.Exit(1)
	}
	llmRouter := buildRouter(providers, cfg.LLMAPIKey, mappingCipher, logger)
	logger.Info("LLM Router инициализирован", "providers", llmRouter.Count())

	handler, orchestrator := api.NewRouter(api.Deps{
		Logger:        logger,
		Store:         store,
		AuthSecret:    cfg.AuthSecret,
		Router:        llmRouter,
		SanitizerURL:  cfg.SanitizerURL,
		MappingCipher: mappingCipher,
	})
	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown (MAJOR-A финального ревью Итерации 9): после
	// получения SIGINT/SIGTERM сначала отказываем новым соединениям,
	// затем ждём фоновые auto-incident-горутины оркестратора и закрываем БД.
	// Без этого Tx3 (CreateAutoIncident) может оборваться на shutdown
	// и нарушить compliance-инвариант полноты audit-trail.
	shutdownCtx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdownCtx.Done()
		logger.Info("rubezh-api: получен сигнал shutdown")
		shutdownTimeout, cancel := context.WithTimeout(
			context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownTimeout); err != nil {
			logger.Error("ошибка graceful shutdown HTTP", "error", err)
		}
	}()

	logger.Info("rubezh-api запущен", "port", cfg.HTTPPort)
	if err := srv.ListenAndServe(); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		logger.Error("ошибка HTTP-сервера", "error", err)
		os.Exit(1)
	}

	// HTTP завершён — ждём фоновые задачи оркестратора (auto-incident).
	logger.Info("rubezh-api: ожидание фоновых задач оркестратора")
	orchestrator.Wait()
	logger.Info("rubezh-api: остановлен корректно")
}

// buildRouter регистрирует провайдеров LLM по конфигурации из БД.
// Отключённые (is_enabled = false) провайдеры пропускаются.
//
// Итерация 9.5: каждый openai_compatible-провайдер использует свой
// зашифрованный api_key (поле api_key_encrypted, AES-GCM с AAD=name).
// Если поле NULL/пусто или расшифровка падает — fallback на envFallback
// (env LLM_API_KEY, deprecated). Это backward-compat для существующих
// deployments, которые ещё не перевели провайдеров на per-key.
//
// Ошибка расшифровки логируется (без plaintext); ключа `redacted`-маркер,
// инвариант "никакого raw в логах" сохраняется через slog.LogValuer
// в storage.ModelProvider.
func buildRouter(
	providers []storage.ModelProvider, envFallback string,
	cipher *crypto.Cipher, logger *slog.Logger,
) *llm.Router {
	router := llm.NewRouter()
	for _, provider := range providers {
		if !provider.IsEnabled {
			continue
		}
		switch provider.Adapter {
		case "openai_compatible":
			key, ok := resolveProviderKey(provider, envFallback, cipher, logger)
			if !ok {
				continue // fail-closed: не регистрируем без ключа
			}
			router.Register(llm.NewOpenAIProvider(
				provider.Name, provider.Endpoint, key))
		default:
			router.Register(llm.NewMockProvider(provider.Name))
		}
	}
	return router
}

// resolveProviderKey расшифровывает api_key провайдера или возвращает
// envFallback **только** если ключ в БД не задан (HasAPIKey()==false).
// При HasAPIKey()==true и ошибке расшифровки — возвращает ("", false):
// провайдер не регистрируется (MAJOR-2 ревью 9.5: fail-closed вместо
// silent fallback на env, который маскировал бы мисконфиг ключа).
// AAD = id (иммутабельный, MINOR-1 ревью 9.5).
func resolveProviderKey(
	provider storage.ModelProvider, envFallback string,
	cipher *crypto.Cipher, logger *slog.Logger,
) (string, bool) {
	if !provider.HasAPIKey() {
		return envFallback, true
	}
	aad := []byte("model_provider_api_key:" + provider.ID)
	plain, err := cipher.Decrypt(provider.APIKeyEncrypted, aad)
	if err != nil {
		logger.Error("api_key провайдера не расшифрован — провайдер пропущен",
			"provider", provider.Name, "id", provider.ID, "error", err)
		return "", false
	}
	return string(plain), true
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
