// Package config загружает конфигурацию сервиса rubezh-api из окружения.
package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// parseRoleMap разбирает OIDC_ROLE_MAP вида "claimval:role,val2:role2" в map.
func parseRoleMap(raw string) map[string]string {
	m := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if ok && k != "" && v != "" {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

// Config — конфигурация сервиса rubezh-api.
type Config struct {
	HTTPPort             string
	LogLevel             string
	DatabaseURL          string
	AuthSecret           string
	SanitizerURL         string
	LLMAPIKey            string
	MappingEncryptionKey []byte // 32 байта; декодирован из base64 env
	MinioEndpoint        string // Итерация 10; пусто → /api/documents 503
	MinioAccessKey       string
	MinioSecretKey       string
	MinioBucket          string
	MinioSecure          bool
	Embedder             EmbedderConfig
	OIDC                 OIDCConfig
	// RAGEnabled — operator-level toggle для RAG в чате (Итерация 11
	// MINOR-4). Default true. false ⇒ retriever не подключается в
	// orchestrator: даже если клиент пришлёт rag.enabled=true, RAG-конвейер
	// не запускается. Используется для on-prem развёртываний, где
	// retrieval должен быть полностью выключен.
	RAGEnabled bool
	// SSHLLM — конфигурация adapter ssh_cli (внешние LLM через bridge на
	// удалённом сервере по SSH; API-ключи не используются). Пусто/Enabled=false
	// → провайдеры с adapter=ssh_cli не регистрируются (fail-closed).
	SSHLLM SSHLLMConfig
	// MetricsScrapeBearer — опциональный Bearer-token для GET /metrics
	// (W4 MJ-1). Пусто → endpoint открыт (on-prem за периметром).
	// Непусто → требует `Authorization: Bearer <value>`.
	MetricsScrapeBearer string
}

// SSHLLMConfig — параметры SSH-CLI bridge для внешних LLM (Codex/Claude/
// Gemini/Grok-CLI на удалённом сервере). Никаких пользовательских ключей и
// паролей здесь нет: аутентификация — ed25519 (KeyPath, монтируется как
// docker-secret). Fail-closed: если Enabled=true, но любой из путей/host/user
// пустой — buildProviders НЕ регистрирует ssh_cli-провайдеров.
type SSHLLMConfig struct {
	Enabled        bool
	Host           string
	Port           int
	User           string
	KeyPath        string // путь к приватному ключу (read-only mount)
	KnownHostsPath string // путь к known_hosts (host pinning)
	RemoteCommand  string // напр. /usr/local/bin/ai-bridge
	Timeout        int    // секунды; default 180
}

// Valid возвращает true, если конфиг полный и пригоден к использованию.
// Используется для fail-closed-логики: ssh_cli-провайдер не регистрируется
// при !Enabled или !Valid().
func (c SSHLLMConfig) Valid() bool {
	return c.Host != "" && c.Port > 0 && c.User != "" &&
		c.KeyPath != "" && c.KnownHostsPath != "" && c.RemoteCommand != ""
}

// EmbedderConfig — конфигурация Embedder'а (Итерация 11 §Р2).
// Kind: "mock" (default) | "openai_compatible".
// URL/Model обязательны при Kind=openai_compatible (иначе сервис
// падает на старте — fail-closed; нельзя молча скатиться к mock и
// порвать симметрию с worker).
type EmbedderConfig struct {
	Kind    string // EMBEDDER_KIND
	URL     string // EMBEDDER_URL (base, без /v1/embeddings)
	Model   string // EMBEDDER_MODEL (имя модели; пишется в embeddings.model)
	APIKey  string // EMBEDDER_API_KEY (опц.; пусто → без Authorization)
	Timeout int    // EMBEDDER_TIMEOUT_SECONDS (default 30)
}

// OIDCConfig — параметры OIDC Relying Party (K.1). Пустой Issuer/ClientID/
// ClientSecret → OIDC выключен, остаётся dev-login.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string            // callback rubezh-api, напр. http://localhost:8080/api/auth/oidc/callback
	FrontendURL  string            // куда вернуть пользователя с токеном, напр. http://localhost:5173
	RoleClaim    string            // claim с ролью/группой (напр. "groups"); пусто → все user
	RoleMap      map[string]string // значение claim → код роли проекта
}

// Enabled — сконфигурирован ли OIDC (все обязательные поля заданы).
func (o OIDCConfig) Enabled() bool {
	return o.Issuer != "" && o.ClientID != "" && o.ClientSecret != "" &&
		o.RedirectURL != ""
}

// Load читает конфигурацию из переменных окружения, подставляя значения по
// умолчанию. Возвращает ошибку, если обязательные параметры не заданы.
func Load() (Config, error) {
	mappingKey, err := decodeMappingKey(os.Getenv("MAPPING_ENCRYPTION_KEY"))
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		HTTPPort:             HTTPPort(),
		LogLevel:             getEnv("API_LOG_LEVEL", "info"),
		DatabaseURL:          databaseURL(),
		AuthSecret:           os.Getenv("AUTH_DEV_TOKEN_SECRET"),
		SanitizerURL:         getEnv("SANITIZER_URL", "http://rubezh-sanitizer:8001"),
		LLMAPIKey:            os.Getenv("LLM_API_KEY"),
		MappingEncryptionKey: mappingKey,
		MinioEndpoint:        os.Getenv("MINIO_ENDPOINT"),
		MinioAccessKey:       os.Getenv("MINIO_ROOT_USER"),
		MinioSecretKey:       os.Getenv("MINIO_ROOT_PASSWORD"),
		MinioBucket:          getEnv("MINIO_BUCKET", "rubezh-documents"),
		MinioSecure:          os.Getenv("MINIO_SECURE") == "true",
		Embedder: EmbedderConfig{
			Kind:    getEnv("EMBEDDER_KIND", "mock"),
			URL:     os.Getenv("EMBEDDER_URL"),
			Model:   os.Getenv("EMBEDDER_MODEL"),
			APIKey:  os.Getenv("EMBEDDER_API_KEY"),
			Timeout: parseIntEnv("EMBEDDER_TIMEOUT_SECONDS", 30),
		},
		OIDC: OIDCConfig{
			Issuer:       os.Getenv("OIDC_ISSUER"),
			ClientID:     os.Getenv("OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("OIDC_REDIRECT_URL"),
			FrontendURL:  getEnv("OIDC_FRONTEND_URL", "http://localhost:5173"),
			RoleClaim:    os.Getenv("OIDC_ROLE_CLAIM"),
			RoleMap:      parseRoleMap(os.Getenv("OIDC_ROLE_MAP")),
		},
		// RAG_ENABLED — only "false" / "0" disables; всё прочее (включая
		// отсутствие env) = true (default-on, удобный для on-prem MVP).
		RAGEnabled:          os.Getenv("RAG_ENABLED") != "false" && os.Getenv("RAG_ENABLED") != "0",
		MetricsScrapeBearer: os.Getenv("METRICS_SCRAPE_BEARER"),
		SSHLLM: SSHLLMConfig{
			Enabled:        os.Getenv("SSH_LLM_ENABLED") == "true",
			Host:           os.Getenv("SSH_LLM_HOST"),
			Port:           parseIntEnv("SSH_LLM_PORT", 22),
			User:           os.Getenv("SSH_LLM_USER"),
			KeyPath:        os.Getenv("SSH_LLM_KEY_PATH"),
			KnownHostsPath: os.Getenv("SSH_LLM_KNOWN_HOSTS_PATH"),
			RemoteCommand: getEnv("SSH_LLM_REMOTE_COMMAND",
				"/usr/local/bin/ai-bridge"),
			Timeout: parseIntEnv("SSH_LLM_TIMEOUT_SECONDS", 180),
		},
	}
	if cfg.AuthSecret == "" {
		return Config{}, fmt.Errorf("config: переменная AUTH_DEV_TOKEN_SECRET обязательна")
	}
	return cfg, nil
}

// decodeMappingKey декодирует base64-значение env MAPPING_ENCRYPTION_KEY
// и проверяет длину == 32 байт (AES-256). Fail-closed: сервис не стартует
// без валидного ключа (план iteration-9.md §Р1).
func decodeMappingKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, fmt.Errorf(
			"config: переменная MAPPING_ENCRYPTION_KEY обязательна " +
				"(base64-кодированный 32-байтовый ключ AES-256)")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf(
			"config: MAPPING_ENCRYPTION_KEY не валидный base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf(
			"config: MAPPING_ENCRYPTION_KEY должен быть ровно 32 байта "+
				"(AES-256); получено %d байт", len(key))
	}
	return key, nil
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

// HTTPPort возвращает порт HTTP-сервера — единый источник для основного
// сервера и для режима healthcheck (исключает рассинхрон).
func HTTPPort() string {
	return getEnv("API_PORT", "8080")
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// parseIntEnv возвращает int из env или fallback при пустой/невалидной
// строке. Не падает на ошибке — только при заведомо обязательных env
// (которые проверяются явно в Load).
func parseIntEnv(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n <= 0 {
		return fallback
	}
	return n
}
