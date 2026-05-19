package storage

import (
	"context"
	"errors"
	"testing"
)

func TestUserIDForRole(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	seen := map[string]string{}
	for _, role := range []string{
		"user", "security_officer", "compliance_officer",
		"admin", "auditor", "developer",
	} {
		id, err := store.UserIDForRole(ctx, role)
		if err != nil {
			t.Fatalf("UserIDForRole(%q): %v", role, err)
		}
		if id == "" {
			t.Errorf("роль %q: пустой id", role)
		}
		if prev, dup := seen[id]; dup {
			t.Errorf("роли %q и %q делят один id %s", role, prev, id)
		}
		seen[id] = role
	}
}

func TestUserIDForRoleUnknown(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	_, err := store.UserIDForRole(context.Background(), "нет-такой-роли")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("неизвестная роль: ожидалась ErrUserNotFound, получено %v", err)
	}
}
