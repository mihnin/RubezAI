package api

import (
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/config"
)

func oidcWithMap(roleClaim string, m map[string]string) *OIDCAuth {
	return &OIDCAuth{cfg: config.OIDCConfig{RoleClaim: roleClaim, RoleMap: m}}
}

func TestMapRoleFromGroupsClaim(t *testing.T) {
	o := oidcWithMap("groups", map[string]string{
		"rubezh-admins": "admin", "rubezh-ib": "security_officer",
	})
	got := o.mapRole(map[string]any{"groups": []any{"x", "rubezh-ib"}})
	if got != "security_officer" {
		t.Errorf("got %q, want security_officer", got)
	}
}

func TestMapRoleStringClaim(t *testing.T) {
	o := oidcWithMap("role", map[string]string{"boss": "admin"})
	if got := o.mapRole(map[string]any{"role": "boss"}); got != "admin" {
		t.Errorf("got %q, want admin", got)
	}
}

func TestMapRoleUnknownDefaultsToUser(t *testing.T) {
	o := oidcWithMap("groups", map[string]string{"rubezh-admins": "admin"})
	if got := o.mapRole(map[string]any{"groups": []any{"strangers"}}); got != "user" {
		t.Errorf("неизвестная группа должна давать user, got %q", got)
	}
}

func TestMapRoleNoClaimConfigDefaultsToUser(t *testing.T) {
	o := oidcWithMap("", nil)
	if got := o.mapRole(map[string]any{"groups": []any{"rubezh-admins"}}); got != "user" {
		t.Errorf("без RoleClaim — least privilege user, got %q", got)
	}
}

func TestMapRoleRejectsInvalidRoleCode(t *testing.T) {
	// маппинг на несуществующую роль игнорируется → least privilege
	o := oidcWithMap("groups", map[string]string{"g": "superadmin"})
	if got := o.mapRole(map[string]any{"groups": []any{"g"}}); got != "user" {
		t.Errorf("невалидная роль должна отбрасываться, got %q", got)
	}
}
