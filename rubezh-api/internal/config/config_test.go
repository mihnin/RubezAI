package config

import "testing"

// clearEnv сбрасывает все переменные окружения config и задаёт обязательный
// AUTH_DEV_TOKEN_SECRET — изолирует тест от внешнего окружения.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"API_PORT", "API_LOG_LEVEL", "DATABASE_URL", "SANITIZER_URL",
		"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_HOST",
		"POSTGRES_PORT", "POSTGRES_DB",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("AUTH_DEV_TOKEN_SECRET", "secret")
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
	if cfg != (Config{}) {
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
