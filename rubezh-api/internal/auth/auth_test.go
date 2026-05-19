package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const testSecret = "test-secret"

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestIssueParseRoundTrip(t *testing.T) {
	token := IssueToken(RoleAdmin, testSecret)
	role, err := ParseToken(token, testSecret)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if role != RoleAdmin {
		t.Errorf("role = %q, ожидалось admin", role)
	}
}

func TestParseRejectsWrongSecret(t *testing.T) {
	token := IssueToken(RoleUser, testSecret)
	if _, err := ParseToken(token, "other-secret"); err == nil {
		t.Error("ожидалась ошибка при неверном секрете")
	}
}

func TestParseRejectsMalformedToken(t *testing.T) {
	for _, bad := range []string{"", "nodot", "admin.", "unknownrole.deadbeef"} {
		if _, err := ParseToken(bad, testSecret); err == nil {
			t.Errorf("ParseToken(%q): ожидалась ошибка", bad)
		}
	}
}

func TestMiddlewareRejectsMissingToken(t *testing.T) {
	rec := httptest.NewRecorder()
	Middleware(testSecret)(okHandler()).ServeHTTP(
		rec, httptest.NewRequest(http.MethodGet, "/", nil),
	)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, ожидалось 401", rec.Code)
	}
}

func TestMiddlewarePassesValidToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+IssueToken(RoleAuditor, testSecret))
	rec := httptest.NewRecorder()
	Middleware(testSecret)(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, ожидалось 200", rec.Code)
	}
}

func TestRoleFromContextAfterMiddleware(t *testing.T) {
	var got Role
	var ok bool
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok = RoleFromContext(r.Context())
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+IssueToken(RoleSecurityOfficer, testSecret))
	Middleware(testSecret)(inner).ServeHTTP(httptest.NewRecorder(), req)
	if !ok || got != RoleSecurityOfficer {
		t.Errorf("роль из контекста = %q, ok=%v", got, ok)
	}
}
