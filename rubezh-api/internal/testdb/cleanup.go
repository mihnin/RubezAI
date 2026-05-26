// Package testdb — общие хелперы для интеграционных тестов с PostgreSQL.
// Используется только в _test.go файлах (storage, api, chat); в обычной
// сборке rubezh-api пакет не импортируется.
//
// Зачем существует: интеграционные тесты пишут в общую compose-БД и
// засоряют её записями (`model_providers`, `policies`,
// `user_provider_credentials`). Этот пакет даёт два инварианта:
//
//  1. `TestNameUnique(t, kind)` — стандартный префикс
//     `itest_<pid>_` для имён, ОТДЕЛЬНЫЙ ДЛЯ КАЖДОГО `go test`-процесса.
//     Это критично: `go test ./...` запускает пакеты параллельно
//     отдельными процессами, и cleanup одного пакета не должен
//     стирать «живые» записи другого пакета.
//  2. `Cleanup(dsn, prefixes)` — пост-прогонная очистка по префиксам.
//     Защищена host-allowlist'ом (anti-prod-БД sanity-check).
//
// CI чист сам по себе (отдельный эфемерный postgres), но локально
// мусор накапливается в compose-postgres между прогонами.
//
// Scope cleanup'а: `model_providers`, `policies`,
// `user_provider_credentials` — то, что мешает UI (picker чата,
// разделы Модели/Политики/Мои Ключи). chat_sessions/chat_messages и
// sanitization_results — short-lived, не попадают в UI как «список»,
// поэтому НЕ чистятся (запись с тестовым session_id видна только
// в самой ChatPage конкретной сессии — пользователь её не увидит).
package testdb

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// devDBHostAllowlist — список host'ов, которые считаются «безопасными
// dev/test» для исполнения мутационного cleanup'а. Если TEST_DATABASE_URL
// указывает на host НЕ из этого списка, Cleanup отказывается работать
// и пишет warning (защита от случайного указания prod-DSN).
//
// KNOWN LIMITATION: имена `postgres` и `db` — типовые в k8s/staging
// (Service name резолвится коротким hostname'ом). На текущей
// compose-only инфраструктуре риск нулевой; при появлении k8s-staging
// добавить TESTDB_ALLOW_HOST override или сузить allowlist через
// явный TESTDB_HOST_ALLOWLIST env.
var devDBHostAllowlist = map[string]bool{
	"postgres":  true, // docker-compose service name
	"localhost": true,
	"127.0.0.1": true,
	"::1":       true,
	"db":        true, // распространённое имя для compose-сервиса
}

// extraAllowedHostEnv — имя env с дополнительным host'ом, который
// разработчик может явно добавить в whitelist (например, для нестандартного
// docker-compose с другим именем сервиса). Значение — один hostname,
// без порта. Пустое значение → ничего не добавляется.
const extraAllowedHostEnv = "TESTDB_ALLOW_HOST"

// legacyPrefixes — исторически сложившийся зоопарк префиксов из старых
// тестов (`test-policy-`, `model-`, `dup-`, `toggle-` и т.п.). Чистка
// идёт ОДНОРАЗОВО — после миграции тестов на TestNameUnique() этот
// список можно убрать.
var legacyPrefixes = []string{
	"test-",
	"dup-",
	"model-",
	"toggle-",
	"del-",
	"nullable-",
	"atomic-",
	"itest-", // дефис-вариант от предыдущих экспериментов
}

// nameCounter обеспечивает уникальность имени даже при двух вызовах в
// одну и ту же наносекунду (на Windows резолюция UnixNano грубая).
var nameCounter atomic.Uint64

// ProcessPrefix возвращает уникальный префикс для текущего test-процесса:
// `itest_<pid>_`. Cleanup чистит ТОЛЬКО записи с этим префиксом — это
// гарантирует, что параллельные `go test`-процессы (разные пакеты)
// не стирают друг другу данные.
func ProcessPrefix() string {
	return fmt.Sprintf("itest_%d_", os.Getpid())
}

// Prefixes возвращает полный список префиксов для cleanup'а: собственный
// per-pid + исторические legacy. Legacy чистится в каждом пакете —
// идемпотентно, никто их новых не создаёт.
func Prefixes() []string {
	return append([]string{ProcessPrefix()}, legacyPrefixes...)
}

// TestNameUnique возвращает уникальное имя `itest_<pid>_<kind>_<nano>_<n>`.
// Глобальный `Cleanup` подберёт запись по префиксу `itest_<pid>_`.
// Atomic counter защищает от коллизии в одну наносекунду.
func TestNameUnique(t *testing.T, kind string) string {
	t.Helper()
	n := nameCounter.Add(1)
	return fmt.Sprintf("%s%s_%d_%d",
		ProcessPrefix(), kind, time.Now().UnixNano(), n)
}

// Cleanup удаляет тестовые артефакты из общей dev-БД по префиксам имён.
// Молча игнорирует ошибки: БД может быть недоступна (нет
// TEST_DATABASE_URL, CI без БД, временный network-сбой) — это не
// должно валить прогон тестов.
//
// SAFETY: исполняется ТОЛЬКО если host из DSN — в `devDBHostAllowlist`.
// Иначе пишет warning в `slog` и выходит без изменений. Это
// защищает от случайного указания production-DSN в TEST_DATABASE_URL.
//
// Семантика по таблицам:
//   - `user_provider_credentials` — DELETE до model_providers. Хотя FK
//     `provider_id` объявлен ON DELETE CASCADE (миграция 000014:9),
//     model_providers мы НЕ удаляем (только soft-disable), поэтому
//     credentials не каскадируются автоматом — чистим явно, иначе
//     мусор останется в разделе «Мои ключи» (как и в picker чата).
//   - `model_providers` — soft-disable (UPDATE is_enabled=false),
//     потому что append-only FK из `audit_events.provider_id` запрещает
//     DELETE (см. `storage.ErrModelProviderReferenced`). Picker чистый —
//     задача выполнена.
//   - `policies` — DELETE (`policy_versions` каскадно).
func Cleanup(dsn string, prefixes []string) {
	if dsn == "" || len(prefixes) == 0 {
		return
	}
	if !isDevDBSafe(dsn) {
		slog.Warn("testdb.Cleanup: TEST_DATABASE_URL host не в allowlist — cleanup пропущен (защита от prod-БД)",
			"allowed_hosts", allowedHosts(),
			"override_env", extraAllowedHostEnv)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return
	}

	likeOnName := BuildNameLikeClause("name", prefixes)
	likeOnProvider := BuildNameLikeClause("mp.name", prefixes)

	// 1) Зависимые FK-записи — до model_providers.
	_, _ = pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM user_provider_credentials
		 WHERE provider_id IN (
			 SELECT id FROM model_providers mp WHERE %s
		 )`, likeOnProvider))

	// 2) model_providers — soft-disable.
	_, _ = pool.Exec(ctx, fmt.Sprintf(
		`UPDATE model_providers
		 SET is_enabled = false
		 WHERE (%s) AND is_enabled = true`, likeOnName))

	// 3) policies — полное удаление.
	_, _ = pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM policies WHERE %s`, likeOnName))
}

// isDevDBSafe парсит DSN и проверяет, что host — в whitelist'е
// dev/test-окружений (с учётом env TESTDB_ALLOW_HOST). Если DSN не
// парсится или scheme неизвестен — возвращает false (fail-safe).
// Поддерживает форматы: `postgres://user:pass@host:port/db`,
// `postgresql://...`, key=value (`host=postgres port=5432 ...`).
func isDevDBSafe(dsn string) bool {
	if strings.HasPrefix(dsn, "postgres://") ||
		strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return false
		}
		return isHostAllowed(u.Hostname())
	}
	// Любая другая строка со схемой `://` — неизвестный формат,
	// отказ безопаснее предположения "local socket".
	if strings.Contains(dsn, "://") {
		return false
	}
	// libpq key=value формат: host=X port=Y ...
	for _, kv := range strings.Fields(dsn) {
		if k, v, ok := strings.Cut(kv, "="); ok && k == "host" {
			return isHostAllowed(v)
		}
	}
	// DSN без явного host — libpq по умолчанию = local socket / localhost.
	return true
}

// isHostAllowed — host из встроенного allowlist'а ИЛИ из env-override.
func isHostAllowed(host string) bool {
	if devDBHostAllowlist[host] {
		return true
	}
	if extra := os.Getenv(extraAllowedHostEnv); extra != "" && extra == host {
		return true
	}
	return false
}

// allowedHosts — отсортированный список всех разрешённых host'ов
// (встроенные + override) для отображения в warning-логе.
func allowedHosts() []string {
	out := make([]string, 0, len(devDBHostAllowlist)+1)
	for k := range devDBHostAllowlist {
		out = append(out, k)
	}
	if extra := os.Getenv(extraAllowedHostEnv); extra != "" {
		out = append(out, extra)
	}
	return out
}

// BuildNameLikeClause собирает выражение "col LIKE 'p1%' OR col LIKE 'p2%'".
// Префиксы — литеральные константы из кода пакета `testdb` или из
// `TestNameUnique` (формат `itest_<pid>_...`). Экранирование одинарной
// кавычки — safety net на случай неосторожной правки списка.
func BuildNameLikeClause(column string, prefixes []string) string {
	parts := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		safe := strings.ReplaceAll(p, "'", "''")
		parts = append(parts, fmt.Sprintf("%s LIKE '%s%%'", column, safe))
	}
	return strings.Join(parts, " OR ")
}
