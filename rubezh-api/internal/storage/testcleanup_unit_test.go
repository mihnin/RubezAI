package storage

import (
	"context"
	"os"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/testdb"
)

// TestCleanupSoftDisablesProviders проверяет, что Cleanup делает
// soft-disable провайдера с тестовым префиксом (DELETE невозможен из-за
// FK от append-only audit_events, см. ErrModelProviderReferenced).
//
// Замечание о race: глобальный TestMain после m.Run() повторно вызовет
// Cleanup с тем же per-pid префиксом — это идемпотентно и НЕ ломает
// тест (запись уже soft-disabled, второй UPDATE — no-op).
func TestCleanupSoftDisablesProviders(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — интеграционный тест БД пропущен")
	}
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	name := testdb.TestNameUnique(t, "cleanup_provider")
	created, err := store.CreateModelProvider(ctx, ModelProvider{
		Name: name, TrustLevel: "external", Adapter: "mock",
	})
	if err != nil {
		t.Fatalf("CreateModelProvider: %v", err)
	}
	if !created.IsEnabled {
		t.Fatalf("по умолчанию должен быть is_enabled=true")
	}

	testdb.Cleanup(dsn, []string{testdb.ProcessPrefix()})

	after, err := store.GetModelProvider(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetModelProvider: %v", err)
	}
	if after.IsEnabled {
		t.Errorf("cleanup не выполнил soft-disable: is_enabled остался true для %q", name)
	}
}

// TestCleanupDeletesPolicies проверяет полное удаление политики с
// тестовым префиксом. Используем прямой COUNT по id (не ListPolicies),
// так как у политик нет GetPolicy и список может быть большим.
func TestCleanupDeletesPolicies(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — интеграционный тест БД пропущен")
	}
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	name := testdb.TestNameUnique(t, "cleanup_policy")
	created, err := store.CreatePolicy(ctx, name, "cleanup test")
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	testdb.Cleanup(dsn, []string{testdb.ProcessPrefix()})

	var count int
	if err := store.Pool().QueryRow(ctx,
		`SELECT count(*) FROM policies WHERE id = $1`, created.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count policies: %v", err)
	}
	if count != 0 {
		t.Errorf("политика %q не удалена cleanup'ом (count=%d)", name, count)
	}
}
