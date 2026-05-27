package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHExecRunnerConfig — конфигурация production SSHRunner'а.
// Все поля обязательны; невалидный конфиг → NewSSHExecRunner возвращает
// ошибку и провайдеры с adapter=ssh_cli НЕ регистрируются (fail-closed).
type SSHExecRunnerConfig struct {
	Host           string
	Port           int
	User           string
	KeyPath        string // приватный ключ ed25519, read-only mount
	KnownHostsPath string // host pinning, read-only mount
	RemoteCommand  string // напр. /usr/local/bin/ai-bridge
	Timeout        time.Duration
}

// SSHExecRunner — production SSHRunner: подключается по SSH с pubkey-
// аутентификацией и known_hosts host pinning, исполняет remote command,
// прокидывает stdin/stdout.
//
// Безопасность:
//   - StrictHostKeyChecking аналог через knownhosts.New (no MITM).
//   - PrivateKey grabbed once при NewSSHExecRunner — после возврата
//     plaintext не остаётся в памяти структуры; signer хранит лишь
//     парсированный ключ (как любая Go программа, использующая x/crypto/ssh).
//   - providerArg валидируется через IsValidSSHProviderArg (anti-injection).
//   - команда строится из жёстко зафиксированных путей и whitelist-арга,
//     shell-интерполяция не выполняется.
type SSHExecRunner struct {
	cfg     SSHExecRunnerConfig
	signer  ssh.Signer
	hostCB  ssh.HostKeyCallback
	logger  *slog.Logger
	addr    string
	timeout time.Duration
}

// NewSSHExecRunner парсит ключ, known_hosts и валидирует конфиг. Ошибка →
// провайдеры ssh_cli не регистрируются (fail-closed на старте сервиса).
func NewSSHExecRunner(
	cfg SSHExecRunnerConfig, logger *slog.Logger,
) (*SSHExecRunner, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Host == "" || cfg.User == "" || cfg.KeyPath == "" ||
		cfg.KnownHostsPath == "" || cfg.RemoteCommand == "" {
		return nil, errors.New(
			"ssh_cli: неполный SSH-конфиг (host/user/key/known_hosts/cmd)")
	}
	if cfg.Port <= 0 {
		cfg.Port = 22
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 180 * time.Second
	}
	keyBytes, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("ssh_cli: read key: %w", err)
	}
	// Очищаем буфер исходных байт после парсинга — best-effort,
	// не гарантирует отсутствие копий в GC, но не оставляет открытым.
	defer func() {
		for i := range keyBytes {
			keyBytes[i] = 0
		}
	}()
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("ssh_cli: parse key: %w", err)
	}
	hostCB, err := knownhosts.New(cfg.KnownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("ssh_cli: known_hosts: %w", err)
	}
	return &SSHExecRunner{
		cfg:     cfg,
		signer:  signer,
		hostCB:  hostCB,
		logger:  logger,
		addr:    fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		timeout: cfg.Timeout,
	}, nil
}

// Run выполняет remote команду через SSH-сессию. providerArg валидируется
// против белого списка; невалидный → ошибка без сетевого вызова.
// stdin → session stdin; stdout → возвращаемое значение. stderr НЕ
// логируется raw — только первые 256 рун с маскировкой, чтобы не утекли
// raw-данные prompt'а через диагностику CLI.
func (r *SSHExecRunner) Run(
	ctx context.Context, providerArg string, stdin []byte,
) ([]byte, error) {
	if !IsValidSSHProviderArg(providerArg) {
		return nil, fmt.Errorf("ssh_cli: invalid provider arg %q", providerArg)
	}
	cfg := &ssh.ClientConfig{
		User:            r.cfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(r.signer)},
		HostKeyCallback: r.hostCB,
		Timeout:         r.timeout,
	}
	dialer := &net.Dialer{Timeout: r.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", r.addr)
	if err != nil {
		return nil, fmt.Errorf("ssh_cli: dial: %w", err)
	}
	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, r.addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ssh_cli: handshake: %w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh_cli: session: %w", err)
	}
	defer func() { _ = session.Close() }()

	session.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Remote-команда: путь жёстко зафиксирован, аргумент в whitelist.
	// shell-интерполяция не нужна; одиночный пробел между аргументами.
	cmd := r.cfg.RemoteCommand + " " + providerArg

	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()

	select {
	case <-ctx.Done():
		// MAJOR-M1 защита: посылаем SIGTERM remote-процессу и закрываем
		// SSH-сессию, после чего ОБЯЗАТЕЛЬНО дренируем `done`. Без drain
		// горутина с session.Run может оставаться висеть, пока сервер не
		// закроет канал — под нагрузкой это goroutine leak. Drain
		// ограничен дополнительным дедлайном, чтобы при глухом канале
		// мы всё-таки вернулись (потеря горутины в этом краевом случае
		// предпочтительнее блокировки вызывающего, и логируется).
		_ = session.Signal(ssh.SIGTERM)
		_ = session.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			r.logger.Warn("ssh_cli: session.Run не завершился после "+
				"close — возможен goroutine leak",
				"host", r.cfg.Host, "remote", providerArg)
		}
		return nil, ctx.Err()
	case err := <-done:
		if err != nil {
			r.logger.Error("ssh_cli: remote command failed",
				"host", r.cfg.Host, "remote", providerArg,
				"stderr_kind", classifyStderr(stderr.Bytes()))
			return nil, fmt.Errorf("ssh_cli: remote command failed")
		}
	}
	return stdout.Bytes(), nil
}

// classifyStderr возвращает структурную метку для лога, чтобы не утекли
// raw-фрагменты (CLI-инструменты иногда печатают prompt в stderr при
// ошибке). Грубая эвристика case-insensitive по известным подстрокам.
// На выход — только структурная метка, никаких фрагментов stderr.
func classifyStderr(stderr []byte) string {
	if len(stderr) == 0 {
		return "empty"
	}
	s := strings.ToLower(string(stderr))
	switch {
	// Узкие маркеры (MINOR-m2): `auth ` (с пробелом) и `login required`
	// — чтобы не ловить `Authorization` (это валидный header, не ошибка).
	case containsAny(s,
		"unauthorized", "401", "403 ", "auth failed", "auth error",
		"authentication failed", "login required", "not logged in"):
		return "auth_error"
	case containsAny(s,
		"enotfound", "could not resolve", "network error",
		"connection refused", "timeout", "deadline exceeded"):
		return "network_error"
	case containsAny(s, "rate limit", "quota", "429", "too many requests"):
		return "rate_limited"
	default:
		return "other"
	}
}

// containsAny — true, если хоть одна из подстрок (lower-case) есть в s
// (тоже lower-case). Вызывающий обязан привести s к нижнему регистру.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
