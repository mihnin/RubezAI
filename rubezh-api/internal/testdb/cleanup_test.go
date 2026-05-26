package testdb

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCleanupNoOpWhenDSNEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Cleanup с пустым DSN не должен паниковать: %v", r)
		}
	}()
	Cleanup("", Prefixes())
	Cleanup("postgres://u:p@postgres/db", nil)
}

func TestBuildNameLikeClauseEscapesQuotes(t *testing.T) {
	clause := BuildNameLikeClause("name", []string{"a'b"})
	want := "name LIKE 'a''b%'"
	if clause != want {
		t.Errorf("got %q, want %q", clause, want)
	}
}

func TestBuildNameLikeClauseJoinsWithOR(t *testing.T) {
	clause := BuildNameLikeClause("name", []string{"x_", "y_"})
	if !strings.Contains(clause, "name LIKE 'x_%'") ||
		!strings.Contains(clause, "name LIKE 'y_%'") ||
		!strings.Contains(clause, " OR ") {
		t.Errorf("OR-конкатенация сломана: %q", clause)
	}
}

func TestBuildNameLikeClauseSingleField(t *testing.T) {
	clause := BuildNameLikeClause("mp.name", []string{"only_"})
	if clause != "mp.name LIKE 'only_%'" {
		t.Errorf("одиночный префикс: got %q", clause)
	}
}

func TestProcessPrefixIsPerPid(t *testing.T) {
	prefix := ProcessPrefix()
	want := fmt.Sprintf("itest_%d_", os.Getpid())
	if prefix != want {
		t.Errorf("got %q, want %q", prefix, want)
	}
}

func TestPrefixesIncludesOwnAndLegacy(t *testing.T) {
	prefixes := Prefixes()
	if len(prefixes) < 2 {
		t.Fatalf("Prefixes() должен включать минимум per-pid + legacy, got %d", len(prefixes))
	}
	if prefixes[0] != ProcessPrefix() {
		t.Errorf("первый элемент должен быть ProcessPrefix(), got %q", prefixes[0])
	}
	// legacy должен присутствовать (исторические префиксы).
	foundLegacy := false
	for _, p := range prefixes[1:] {
		if p == "test-" {
			foundLegacy = true
		}
	}
	if !foundLegacy {
		t.Error("legacy префиксы не добавлены — старый мусор не будет очищен")
	}
}

func TestTestNameUniqueHasProcessPrefix(t *testing.T) {
	name := TestNameUnique(t, "kind")
	if !strings.HasPrefix(name, ProcessPrefix()+"kind_") {
		t.Errorf("got %q, ожидался префикс %q", name, ProcessPrefix()+"kind_")
	}
}

// TestTestNameUniqueResistsTimestampCollision проверяет, что atomic
// counter защищает от коллизии при двух вызовах в одну наносекунду
// (на Windows резолюция UnixNano грубая, что приводило бы к нарушению
// UNIQUE(name) в model_providers/policies).
func TestTestNameUniqueResistsTimestampCollision(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		name := TestNameUnique(t, "race")
		if seen[name] {
			t.Fatalf("дубликат имени на итерации %d: %q", i, name)
		}
		seen[name] = true
	}
}

func TestIsDevDBSafeWhitelistsLocalhost(t *testing.T) {
	cases := []struct {
		dsn  string
		want bool
		desc string
	}{
		{"postgres://u:p@postgres:5432/db?sslmode=disable", true, "compose service"},
		{"postgres://u:p@localhost:5432/db", true, "localhost"},
		{"postgres://u:p@127.0.0.1:5432/db", true, "loopback IPv4"},
		{"postgresql://u:p@db/db", true, "alias 'db'"},
		{"postgres://u:p@prod-db.example.com:5432/db", false, "prod host"},
		{"postgres://u:p@10.0.5.42:5432/db", false, "private IP"},
		{"host=postgres user=u password=p dbname=db", true, "key=value with safe host"},
		{"host=prod-db.example.com user=u dbname=db", false, "key=value with prod host"},
		{"user=u dbname=db", true, "key=value without host = local"},
		{"://invalid", false, "malformed DSN — fail-safe"},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			if got := isDevDBSafe(c.dsn); got != c.want {
				t.Errorf("isDevDBSafe(%q) = %v, want %v", c.dsn, got, c.want)
			}
		})
	}
}

// TestCleanupRefusesOnUnsafeHost проверяет, что Cleanup НЕ исполнит
// мутационный SQL, если в TEST_DATABASE_URL указан prod-host. Подаём
// заведомо недоступный prod-host — если бы защита не сработала,
// pgxpool.New дошёл бы до Ping и вернулся (молча), а с защитой —
// выходит мгновенно без попытки соединения (наблюдаемо по latency
// в худшем случае; здесь проверяем только отсутствие panic).
func TestCleanupRefusesOnUnsafeHost(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Cleanup на unsafe host не должен паниковать: %v", r)
		}
	}()
	Cleanup("postgres://u:p@prod-db.example.com:5432/db", Prefixes())
}

// TestExtraAllowHostEnvAllowsCustom проверяет, что env-override
// TESTDB_ALLOW_HOST расширяет allowlist (для нестандартных compose-имён).
func TestExtraAllowHostEnvAllowsCustom(t *testing.T) {
	t.Setenv(extraAllowedHostEnv, "my-compose-db")
	if !isDevDBSafe("postgres://u:p@my-compose-db:5432/db") {
		t.Error("env-override TESTDB_ALLOW_HOST не сработал")
	}
	// Без env — должен отказать.
	t.Setenv(extraAllowedHostEnv, "")
	if isDevDBSafe("postgres://u:p@my-compose-db:5432/db") {
		t.Error("после сброса env host больше не должен быть разрешён")
	}
}
