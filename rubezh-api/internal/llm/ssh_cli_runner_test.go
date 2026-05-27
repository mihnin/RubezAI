package llm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFile — упрощает создание временных файлов в тестах.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestNewSSHExecRunnerRejectsIncompleteConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  SSHExecRunnerConfig
	}{
		{"без host", SSHExecRunnerConfig{
			User: "u", KeyPath: "k", KnownHostsPath: "kh", RemoteCommand: "rc",
		}},
		{"без user", SSHExecRunnerConfig{
			Host: "h", KeyPath: "k", KnownHostsPath: "kh", RemoteCommand: "rc",
		}},
		{"без key", SSHExecRunnerConfig{
			Host: "h", User: "u", KnownHostsPath: "kh", RemoteCommand: "rc",
		}},
		{"без known_hosts", SSHExecRunnerConfig{
			Host: "h", User: "u", KeyPath: "k", RemoteCommand: "rc",
		}},
		{"без remote cmd", SSHExecRunnerConfig{
			Host: "h", User: "u", KeyPath: "k", KnownHostsPath: "kh",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSSHExecRunner(tc.cfg, discardLogger())
			if err == nil {
				t.Fatal("ожидалась ошибка на неполный конфиг")
			}
		})
	}
}

func TestNewSSHExecRunnerRejectsMissingKey(t *testing.T) {
	dir := t.TempDir()
	cfg := SSHExecRunnerConfig{
		Host: "h", Port: 22, User: "u",
		KeyPath:        filepath.Join(dir, "no-such-key"),
		KnownHostsPath: writeFile(t, dir, "known_hosts", ""),
		RemoteCommand:  "/usr/local/bin/ai-bridge",
		Timeout:        10 * time.Second,
	}
	_, err := NewSSHExecRunner(cfg, discardLogger())
	if err == nil {
		t.Fatal("ожидалась ошибка на отсутствующий ключ")
	}
	if !strings.Contains(err.Error(), "key") {
		t.Errorf("ошибка должна упоминать ключ: %v", err)
	}
}

func TestNewSSHExecRunnerRejectsBadKey(t *testing.T) {
	dir := t.TempDir()
	cfg := SSHExecRunnerConfig{
		Host: "h", Port: 22, User: "u",
		KeyPath:        writeFile(t, dir, "bad_key", "this is not a key"),
		KnownHostsPath: writeFile(t, dir, "known_hosts", ""),
		RemoteCommand:  "/usr/local/bin/ai-bridge",
	}
	_, err := NewSSHExecRunner(cfg, discardLogger())
	if err == nil {
		t.Fatal("ожидалась ошибка на невалидный ключ")
	}
}

func TestSSHExecRunnerRunRejectsInvalidProviderArg(t *testing.T) {
	// Не нужно валидное соединение: проверка providerArg идёт ДО dial.
	r := &SSHExecRunner{cfg: SSHExecRunnerConfig{Host: "127.0.0.1"}}
	_, err := r.Run(context.Background(), "evil; rm -rf /", []byte("{}"))
	if err == nil {
		t.Fatal("невалидный providerArg должен давать ошибку до сетевого вызова")
	}
	if !strings.Contains(err.Error(), "provider arg") {
		t.Errorf("ошибка должна упоминать provider arg: %v", err)
	}
}

func TestClassifyStderr(t *testing.T) {
	cases := map[string]string{
		"":                               "empty",
		"Error: 401 Unauthorized":        "auth_error",
		"Login required":                 "auth_error",
		"authentication failed: bad key": "auth_error",
		"not logged in to claude":        "auth_error",
		"ENOTFOUND api.x.ai":             "network_error",
		"Connection refused":             "network_error",
		"deadline exceeded":              "network_error",
		"Rate limit exceeded":            "rate_limited",
		"HTTP 429 Too Many Requests":     "rate_limited",
		"что-то странное":                "other",
		// false-positive guard (MINOR-m2): "Authorization" — валидный
		// заголовок, не должен мапиться в auth_error.
		"Set Authorization: Bearer xxx": "other",
	}
	for in, want := range cases {
		got := classifyStderr([]byte(in))
		if got != want {
			t.Errorf("classifyStderr(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("hello world", "world", "missing") {
		t.Error("должно найти первое совпадение")
	}
	if containsAny("hello", "x", "y") {
		t.Error("не должно найти")
	}
	if containsAny("hello") {
		t.Error("без аргументов — false")
	}
	if containsAny("hello", "") {
		t.Error("пустая подстрока игнорируется")
	}
}
