package config

import (
	"encoding/base64"
	"testing"
)

// validMappingKey — валидный 32-байтовый ключ для тестов (base64).
const validMappingKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" // 32 нулевых байта

// clearEnv сбрасывает все переменные окружения config и задаёт обязательный
// AUTH_DEV_TOKEN_SECRET + MAPPING_ENCRYPTION_KEY — изолирует тест от внешнего окружения.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"API_PORT", "API_LOG_LEVEL", "DATABASE_URL", "SANITIZER_URL",
		"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_HOST",
		"POSTGRES_PORT", "POSTGRES_DB",
		"EMBEDDER_KIND", "EMBEDDER_URL", "EMBEDDER_MODEL",
		"EMBEDDER_API_KEY", "EMBEDDER_TIMEOUT_SECONDS",
		"SSH_LLM_ENABLED", "SSH_LLM_HOST", "SSH_LLM_PORT", "SSH_LLM_USER",
		"SSH_LLM_KEY_PATH", "SSH_LLM_KNOWN_HOSTS_PATH",
		"SSH_LLM_REMOTE_COMMAND", "SSH_LLM_TIMEOUT_SECONDS",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("AUTH_DEV_TOKEN_SECRET", "secret")
	t.Setenv("MAPPING_ENCRYPTION_KEY", validMappingKey)
}

func TestLoadAppliesDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort = %q, ожидалось 8080", cfg.HTTPPort)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, ожидалось info", cfg.LogLevel)
	}
	if cfg.SanitizerURL != "http://rubezh-sanitizer:8001" {
		t.Errorf("SanitizerURL = %q", cfg.SanitizerURL)
	}
}

func TestLoadReadsEnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("API_PORT", "9090")
	t.Setenv("API_LOG_LEVEL", "debug")
	t.Setenv("SANITIZER_URL", "http://custom:9000")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPPort != "9090" || cfg.LogLevel != "debug" {
		t.Errorf("override не применён: %+v", cfg)
	}
	if cfg.SanitizerURL != "http://custom:9000" {
		t.Errorf("SanitizerURL = %q", cfg.SanitizerURL)
	}
}

func TestLoadRequiresAuthSecret(t *testing.T) {
	clearEnv(t)
	t.Setenv("AUTH_DEV_TOKEN_SECRET", "")
	t.Setenv("API_PORT", "9090")
	cfg, err := Load()
	if err == nil {
		t.Fatal("ожидалась ошибка при отсутствии AUTH_DEV_TOKEN_SECRET")
	}
	// Config содержит []byte — сравнение через DeepEqual или ключевые поля.
	if cfg.AuthSecret != "" || cfg.HTTPPort != "" ||
		len(cfg.MappingEncryptionKey) != 0 {
		t.Errorf("при ошибке ожидался нулевой Config, получено %+v", cfg)
	}
}

func TestLoadBuildsDatabaseURLFromParts(t *testing.T) {
	clearEnv(t)
	t.Setenv("POSTGRES_USER", "u")
	t.Setenv("POSTGRES_PASSWORD", "p")
	t.Setenv("POSTGRES_HOST", "h")
	t.Setenv("POSTGRES_PORT", "6543")
	t.Setenv("POSTGRES_DB", "d")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := "postgres://u:p@h:6543/d?sslmode=disable"
	if cfg.DatabaseURL != want {
		t.Errorf("DatabaseURL = %q, ожидалось %q", cfg.DatabaseURL, want)
	}
}

func TestLoadDatabaseURLDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := "postgres://rubezh:rubezh@postgres:5432/rubezh?sslmode=disable"
	if cfg.DatabaseURL != want {
		t.Errorf("DatabaseURL = %q, ожидалось %q", cfg.DatabaseURL, want)
	}
}

func TestLoadDatabaseURLPrioritizesExplicit(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://explicit:dsn@host/db")
	t.Setenv("POSTGRES_USER", "ignored")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL != "postgres://explicit:dsn@host/db" {
		t.Errorf("DATABASE_URL не имеет приоритета: %q", cfg.DatabaseURL)
	}
}

// TestLoadRequiresMappingKey — пустой MAPPING_ENCRYPTION_KEY → ошибка
// (план iteration-9.md §Р1: fail-closed на старте без ключа).
func TestLoadRequiresMappingKey(t *testing.T) {
	clearEnv(t)
	t.Setenv("MAPPING_ENCRYPTION_KEY", "")
	_, err := Load()
	if err == nil {
		t.Fatal("ожидалась ошибка при отсутствии MAPPING_ENCRYPTION_KEY")
	}
}

// TestLoadRejectsBadMappingKey — невалидный base64 или неправильная длина.
func TestLoadRejectsBadMappingKey(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"не base64", "not-a-base64-!@#$"},
		{"16 байт (AES-128)", base64.StdEncoding.EncodeToString(make([]byte, 16))},
		{"24 байта (AES-192)", base64.StdEncoding.EncodeToString(make([]byte, 24))},
		{"31 байт (короткий)", base64.StdEncoding.EncodeToString(make([]byte, 31))},
		{"33 байта (длинный)", base64.StdEncoding.EncodeToString(make([]byte, 33))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("MAPPING_ENCRYPTION_KEY", tc.raw)
			_, err := Load()
			if err == nil {
				t.Errorf("ожидалась ошибка для %q", tc.raw)
			}
		})
	}
}

// TestLoadDecodesValidMappingKey — валидный ключ декодирован в 32 байта.
func TestLoadDecodesValidMappingKey(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.MappingEncryptionKey) != 32 {
		t.Errorf("длина ключа = %d, ожидалось 32",
			len(cfg.MappingEncryptionKey))
	}
}

func TestLoadEmbedderDefaultsToMock(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Embedder.Kind != "mock" {
		t.Errorf("Kind = %q, ожидалось mock", cfg.Embedder.Kind)
	}
	if cfg.Embedder.Timeout != 30 {
		t.Errorf("Timeout = %d, ожидалось 30", cfg.Embedder.Timeout)
	}
}

func TestLoadEmbedderOpenAICompatible(t *testing.T) {
	clearEnv(t)
	t.Setenv("EMBEDDER_KIND", "openai_compatible")
	t.Setenv("EMBEDDER_URL", "http://lm:1234")
	t.Setenv("EMBEDDER_MODEL", "bge-m3")
	t.Setenv("EMBEDDER_API_KEY", "sk")
	t.Setenv("EMBEDDER_TIMEOUT_SECONDS", "15")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Embedder.Kind != "openai_compatible" {
		t.Errorf("Kind = %q", cfg.Embedder.Kind)
	}
	if cfg.Embedder.URL != "http://lm:1234" {
		t.Errorf("URL = %q", cfg.Embedder.URL)
	}
	if cfg.Embedder.Model != "bge-m3" {
		t.Errorf("Model = %q", cfg.Embedder.Model)
	}
	if cfg.Embedder.APIKey != "sk" {
		t.Errorf("APIKey = %q", cfg.Embedder.APIKey)
	}
	if cfg.Embedder.Timeout != 15 {
		t.Errorf("Timeout = %d", cfg.Embedder.Timeout)
	}
}

func TestParseIntEnvFallbackOnInvalid(t *testing.T) {
	clearEnv(t)
	t.Setenv("EMBEDDER_TIMEOUT_SECONDS", "not-a-number")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Embedder.Timeout != 30 {
		t.Errorf("Timeout = %d, ожидался fallback 30", cfg.Embedder.Timeout)
	}
}

// --- SSH-LLM bridge config (adapter ssh_cli) ---

func TestLoadSSHLLMDefaultsDisabled(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SSHLLM.Enabled {
		t.Error("default SSH_LLM_ENABLED должен быть false")
	}
	if cfg.SSHLLM.Port != 22 {
		t.Errorf("default Port = %d, ожидалось 22", cfg.SSHLLM.Port)
	}
	if cfg.SSHLLM.Timeout != 180 {
		t.Errorf("default Timeout = %d, ожидалось 180", cfg.SSHLLM.Timeout)
	}
	if cfg.SSHLLM.RemoteCommand != "/usr/local/bin/ai-bridge" {
		t.Errorf("default RemoteCommand = %q", cfg.SSHLLM.RemoteCommand)
	}
	if cfg.SSHLLM.Valid() {
		t.Error("дефолтный (пустой) конфиг не должен быть Valid")
	}
}

func TestLoadSSHLLMFullConfig(t *testing.T) {
	clearEnv(t)
	t.Setenv("SSH_LLM_ENABLED", "true")
	t.Setenv("SSH_LLM_HOST", "193.124.93.157")
	t.Setenv("SSH_LLM_PORT", "2222")
	t.Setenv("SSH_LLM_USER", "aiagent")
	t.Setenv("SSH_LLM_KEY_PATH", "/run/secrets/key")
	t.Setenv("SSH_LLM_KNOWN_HOSTS_PATH", "/run/secrets/known_hosts")
	t.Setenv("SSH_LLM_REMOTE_COMMAND", "/usr/local/bin/ai-bridge")
	t.Setenv("SSH_LLM_TIMEOUT_SECONDS", "90")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := cfg.SSHLLM
	if !s.Enabled {
		t.Error("Enabled должен быть true")
	}
	if s.Host != "193.124.93.157" || s.Port != 2222 || s.User != "aiagent" {
		t.Errorf("host/port/user не совпали: %+v", s)
	}
	if s.KeyPath != "/run/secrets/key" ||
		s.KnownHostsPath != "/run/secrets/known_hosts" {
		t.Errorf("KeyPath/KnownHosts = %q / %q", s.KeyPath, s.KnownHostsPath)
	}
	if s.Timeout != 90 {
		t.Errorf("Timeout = %d, ожидалось 90", s.Timeout)
	}
	if !s.Valid() {
		t.Error("полный конфиг должен быть Valid")
	}
}

func TestSSHLLMValidFailClosed(t *testing.T) {
	full := SSHLLMConfig{
		Enabled: true, Host: "h", Port: 22, User: "u",
		KeyPath: "k", KnownHostsPath: "kh", RemoteCommand: "rc", Timeout: 30,
	}
	if !full.Valid() {
		t.Fatal("эталонный конфиг должен быть Valid")
	}
	cases := []struct {
		name string
		mut  func(c *SSHLLMConfig)
	}{
		{"без host", func(c *SSHLLMConfig) { c.Host = "" }},
		{"без user", func(c *SSHLLMConfig) { c.User = "" }},
		{"без key path", func(c *SSHLLMConfig) { c.KeyPath = "" }},
		{"без known hosts", func(c *SSHLLMConfig) { c.KnownHostsPath = "" }},
		{"без remote cmd", func(c *SSHLLMConfig) { c.RemoteCommand = "" }},
		{"port 0", func(c *SSHLLMConfig) { c.Port = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := full
			tc.mut(&c)
			if c.Valid() {
				t.Errorf("%s: ожидалось Valid()=false, получено true", tc.name)
			}
		})
	}
}

func TestParseIntEnvFallbackOnZeroOrNegative(t *testing.T) {
	clearEnv(t)
	t.Setenv("EMBEDDER_TIMEOUT_SECONDS", "0")
	cfg, _ := Load()
	if cfg.Embedder.Timeout != 30 {
		t.Errorf("0 → fallback 30, got %d", cfg.Embedder.Timeout)
	}
	t.Setenv("EMBEDDER_TIMEOUT_SECONDS", "-5")
	cfg, _ = Load()
	if cfg.Embedder.Timeout != 30 {
		t.Errorf("-5 → fallback 30, got %d", cfg.Embedder.Timeout)
	}
}
