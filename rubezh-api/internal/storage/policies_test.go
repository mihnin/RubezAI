package storage

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"
)

// testStore открывает подключение к тестовой БД или пропускает тест,
// если TEST_DATABASE_URL не задан (интеграционный тест требует PostgreSQL).
func testStore(t *testing.T) *Storage {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — интеграционный тест БД пропущен")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.Ping(context.Background()); err != nil {
		store.Close()
		t.Skipf("БД недоступна: %v", err)
	}
	return store
}

func TestCreateAndListPolicies(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	name := "test-policy-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	created, err := store.CreatePolicy(ctx, name, "интеграционный тест")
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	if created.Name != name {
		t.Errorf("Name = %q, ожидалось %q", created.Name, name)
	}
	if created.CurrentVersion != 1 {
		t.Errorf("CurrentVersion = %d, ожидалось 1", created.CurrentVersion)
	}
	if created.ID == "" {
		t.Error("ID не присвоен")
	}

	policies, err := store.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	found := false
	for _, p := range policies {
		if p.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Error("созданная политика отсутствует в списке")
	}
}

func TestCreatePolicyRejectsDuplicateName(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	name := "dup-policy-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if _, err := store.CreatePolicy(ctx, name, ""); err != nil {
		t.Fatalf("первое создание: %v", err)
	}
	if _, err := store.CreatePolicy(ctx, name, ""); !errors.Is(err, ErrPolicyExists) {
		t.Errorf("повторное имя: ожидалась ErrPolicyExists, получено %v", err)
	}
}

func TestCreatePolicyIsAtomic(t *testing.T) {
	// политика и её первая версия создаются атомарно — ровно одна версия
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	name := "atomic-policy-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	created, err := store.CreatePolicy(ctx, name, "")
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	var versions int
	if err := store.Pool().QueryRow(ctx,
		"SELECT count(*) FROM policy_versions WHERE policy_id = $1", created.ID,
	).Scan(&versions); err != nil {
		t.Fatalf("запрос версий: %v", err)
	}
	if versions != 1 {
		t.Errorf("версий политики = %d, ожидалась ровно 1", versions)
	}
}
