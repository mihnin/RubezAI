package config

import "testing"

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("AUTH_DEV_TOKEN_SECRET", "secret")
	t.Setenv("API_PORT", "")
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
}

func TestLoadReadsEnv(t *testing.T) {
	t.Setenv("AUTH_DEV_TOKEN_SECRET", "secret")
	t.Setenv("API_PORT", "9090")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPPort != "9090" {
		t.Errorf("HTTPPort = %q, ожидалось 9090", cfg.HTTPPort)
	}
}

func TestLoadRequiresAuthSecret(t *testing.T) {
	t.Setenv("AUTH_DEV_TOKEN_SECRET", "")
	if _, err := Load(); err == nil {
		t.Error("ожидалась ошибка при отсутствии AUTH_DEV_TOKEN_SECRET")
	}
}

func TestLoadBuildsDatabaseURL(t *testing.T) {
	t.Setenv("AUTH_DEV_TOKEN_SECRET", "secret")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("POSTGRES_USER", "user1")
	t.Setenv("POSTGRES_PASSWORD", "pass1")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL == "" {
		t.Fatal("DatabaseURL пуст")
	}
	want := "postgres://user1:pass1@"
	if len(cfg.DatabaseURL) < len(want) || cfg.DatabaseURL[:len(want)] != want {
		t.Errorf("DatabaseURL = %q, ожидался префикс %q", cfg.DatabaseURL, want)
	}
}
