// Команда rubezh-api — API Gateway «Рубеж ИИ».
package main

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/rubezh-ai/rubezh-api/internal/metrics"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// buildEmbedder создаёт Embedder по конфигу. fail-closed: если выбран
// openai_compatible, но URL/Model не заданы — возвращает ошибку
// (сервис не стартует — нельзя молча скатываться к mock и порвать
// симметрию с worker'ом, который читает те же env).
//
// Поддерживаемые значения EMBEDDER_KIND:
//   - ""              → mock (default; обратная совместимость);
//   - "mock"          → MockEmbedder (детерминированный SHA-256);
//   - "openai_compatible" → OpenAICompatibleEmbedder (LM Studio /
//     vLLM / Ollama); требует EMBEDDER_URL + EMBEDDER_MODEL.
func buildEmbedder(cfg config.EmbedderConfig) (llm.Embedder, error) {
	switch cfg.Kind {
	case "", "mock":
		return llm.MockEmbedder{}, nil
	case "openai_compatible":
		if cfg.URL == "" {
			return nil, fmt.Errorf(
				"config: EMBEDDER_URL обязателен при EMBEDDER_KIND=openai_compatible")
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf(
				"config: EMBEDDER_MODEL обязателен при EMBEDDER_KIND=openai_compatible")
		}
		timeout := time.Duration(cfg.Timeout) * time.Second
		return llm.NewOpenAICompatibleEmbedder(
			cfg.URL, cfg.Model, cfg.APIKey, timeout), nil
	default:
		return nil, fmt.Errorf(
			"config: EMBEDDER_KIND=%q не поддерживается "+
				"(допустимо: mock, openai_compatible)", cfg.Kind)
	}
}

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
	sshRunner := buildSSHRunner(cfg.SSHLLM, logger)
	llmRouter := buildRouter(
		providers, cfg.LLMAPIKey, mappingCipher, sshRunner, logger)
	logger.Info("LLM Router инициализирован", "providers", llmRouter.Count())

	// reloadRouter — hot-reload Router'а из БД после CREATE/UPDATE
	// провайдера (F2). Атомарная подмена набора через Router.Replace.
	// Замыкание дёргает metricsCollector.LLMRouterProviders, поэтому
	// объявляется ПОСЛЕ создания metricsCollector (см. ниже).
	var metricsCollector *metrics.Metrics
	reloadRouter := func(ctx context.Context) error {
		fresh, err := store.ListModelProviders(ctx)
		if err != nil {
			return fmt.Errorf("reload: %w", err)
		}
		llmRouter.Replace(buildProviders(
			fresh, cfg.LLMAPIKey, mappingCipher, sshRunner, logger))
		count := llmRouter.Count()
		logger.Info("LLM Router перезагружен", "providers", count)
		if metricsCollector != nil {
			metricsCollector.LLMRouterProviders.Set(float64(count))
		}
		return nil
	}

	// MinIO для документов (Итерация 10). Опционально — если env
	// MINIO_ENDPOINT не задан, эндпойнты /api/documents отдают 503.
	var minioClient *storage.MinioClient
	if cfg.MinioEndpoint != "" {
		mc, err := storage.NewMinioClient(storage.MinioConfig{
			Endpoint: cfg.MinioEndpoint, AccessKey: cfg.MinioAccessKey,
			SecretKey: cfg.MinioSecretKey, Bucket: cfg.MinioBucket,
			Secure: cfg.MinioSecure,
		})
		if err != nil {
			logger.Error("MinIO init failed", "error", err)
			os.Exit(1)
		}
		if err := mc.EnsureBucket(ctx); err != nil {
			logger.Error("MinIO bucket ensure failed", "error", err)
			os.Exit(1)
		}
		minioClient = mc
	}

	// OIDC RP (K.1) — опционально; ошибка построения не валит сервис
	// (остаётся dev-login), но логируется.
	var oidcAuth *api.OIDCAuth
	if cfg.OIDC.Enabled() {
		oa, oerr := api.NewOIDCAuth(ctx, cfg.OIDC, store, cfg.AuthSecret, logger)
		if oerr != nil {
			logger.Error("OIDC не инициализирован (остаётся dev-login)",
				"issuer", cfg.OIDC.Issuer, "error", oerr)
		} else {
			oidcAuth = oa
			logger.Info("OIDC RP включён", "issuer", cfg.OIDC.Issuer)
		}
	}

	embedder, err := buildEmbedder(cfg.Embedder)
	if err != nil {
		logger.Error("ошибка инициализации Embedder", "error", err)
		os.Exit(1)
	}
	logger.Info("Embedder инициализирован",
		"kind", cfg.Embedder.Kind, "model", embedder.Name(),
		"dim", embedder.Dim())

	// W4.1: Prometheus-инструментация. Создаём один экземпляр; роутер
	// и orchestrator получают его через api.Deps.Metrics для Inc/Observe.
	// metricsCollector — var выше (используется в reloadRouter-замыкании).
	// W4 MJ-1: опциональная защита /metrics Bearer-токеном (env
	// METRICS_SCRAPE_BEARER). Пусто → endpoint открыт.
	metricsCollector = metrics.New().WithScrapeBearer(cfg.MetricsScrapeBearer)
	metricsCollector.LLMRouterProviders.Set(float64(llmRouter.Count()))

	handler, orchestrator := api.NewRouter(api.Deps{
		Logger:        logger,
		Store:         store,
		AuthSecret:    cfg.AuthSecret,
		Router:        llmRouter,
		SanitizerURL:  cfg.SanitizerURL,
		MappingCipher: mappingCipher,
		Minio:         minioClient,
		Embedder:      embedder,
		ReloadRouter:  reloadRouter,
		OIDC:          oidcAuth,
		RAGEnabled:    cfg.RAGEnabled,
		Metrics:       metricsCollector,
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
	cipher *crypto.Cipher, sshRunner llm.SSHRunner, logger *slog.Logger,
) *llm.Router {
	router := llm.NewRouter()
	router.Replace(buildProviders(
		providers, envFallback, cipher, sshRunner, logger))
	return router
}

// buildProviders — общая логика выбора активных провайдеров и
// разрешения api_key. Вынесена отдельно для переиспользования в
// reloadRouter (F2): на hot-reload набор провайдеров пересобирается
// тем же путём, что и при старте, и атомарно подменяется через
// Router.Replace.
func buildProviders(
	providers []storage.ModelProvider, envFallback string,
	cipher *crypto.Cipher, sshRunner llm.SSHRunner, logger *slog.Logger,
) []llm.Provider {
	out := make([]llm.Provider, 0, len(providers))
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
			out = append(out, llm.NewOpenAIProvider(
				provider.Name, provider.Endpoint, key))
		case "anthropic":
			// Claude (Messages API). Ключ обязателен (если не пройдёт
			// resolveProviderKey — fail-closed, как у openai).
			key, ok := resolveProviderKey(provider, envFallback, cipher, logger)
			if !ok {
				continue
			}
			out = append(out, llm.NewAnthropicProvider(
				provider.Name, provider.Endpoint, key))
		case "ssh_cli":
			// Внешние LLM через CLI-bridge на удалённом сервере (Codex/
			// Claude/Gemini/Grok CLI, залогинены серверной учёткой). API-
			// ключи не используются: аутентификация на сервере, репозиторий
			// не хранит секретов. fail-closed: без runner'а
			// (SSH_LLM_ENABLED=false или неполный конфиг) — пропуск с warn.
			if sshRunner == nil {
				logger.Warn("ssh_cli провайдер пропущен — SSH-конфиг "+
					"отключён или неполный (fail-closed)",
					"provider", provider.Name, "id", provider.ID)
				continue
			}
			if !llm.IsValidSSHProviderArg(provider.Endpoint) {
				logger.Error("ssh_cli провайдер пропущен — endpoint вне "+
					"белого списка (codex|claude|gemini|grok|grok-build)",
					"provider", provider.Name, "endpoint", provider.Endpoint)
				continue
			}
			out = append(out, llm.NewSSHCLIProvider(
				provider.Name, provider.Endpoint, sshRunner, logger))
		default:
			out = append(out, llm.NewMockProvider(provider.Name))
		}
	}
	return out
}

// buildSSHRunner создаёт production SSH-runner или nil, если adapter
// ssh_cli отключён/недонастроен. nil здесь — нормальный путь: провайдеры
// с adapter=ssh_cli не зарегистрируются в Router (fail-closed).
// Ошибка инициализации runner'а при включенном SSH_LLM_ENABLED логируется
// и тоже даёт nil — сервис стартует без ssh_cli, не падает целиком.
func buildSSHRunner(
	cfg config.SSHLLMConfig, logger *slog.Logger,
) llm.SSHRunner {
	if !cfg.Enabled {
		logger.Info("ssh_cli: bridge отключён (SSH_LLM_ENABLED=false)")
		return nil
	}
	if !cfg.Valid() {
		logger.Warn("ssh_cli: bridge включён, но конфиг неполный — " +
			"провайдеры ssh_cli не будут зарегистрированы")
		return nil
	}
	runner, err := llm.NewSSHExecRunner(llm.SSHExecRunnerConfig{
		Host:           cfg.Host,
		Port:           cfg.Port,
		User:           cfg.User,
		KeyPath:        cfg.KeyPath,
		KnownHostsPath: cfg.KnownHostsPath,
		RemoteCommand:  cfg.RemoteCommand,
		Timeout:        time.Duration(cfg.Timeout) * time.Second,
	}, logger)
	if err != nil {
		logger.Error("ssh_cli: NewSSHExecRunner failed — "+
			"провайдеры ssh_cli не зарегистрированы", "error", err)
		return nil
	}
	logger.Info("ssh_cli: SSH-bridge runner готов",
		"host", cfg.Host, "user", cfg.User, "cmd", cfg.RemoteCommand)
	return runner
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
