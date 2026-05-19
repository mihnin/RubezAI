package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testSecret = "test-secret"

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func allRoles() []Role {
	return []Role{
		RoleUser, RoleSecurityOfficer, RoleComplianceOfficer,
		RoleAdmin, RoleAuditor, RoleDeveloper,
	}
}

func flipLast(s string) string {
	if s == "" {
		return "0"
	}
	repl := byte('0')
	if s[len(s)-1] == '0' {
		repl = '1'
	}
	return s[:len(s)-1] + string(repl)
}

func TestIssueParseRoundTripAllRoles(t *testing.T) {
	for _, role := range allRoles() {
		got, err := ParseToken(IssueToken(role, testSecret), testSecret)
		if err != nil {
			t.Errorf("роль %q: ParseToken: %v", role, err)
		}
		if got != role {
			t.Errorf("роль %q: получено %q", role, got)
		}
	}
}

func TestParseRejectsInvalidTokens(t *testing.T) {
	userToken := IssueToken(RoleUser, testSecret)
	userSig := userToken[strings.IndexByte(userToken, '.')+1:]
	cases := []struct {
		name  string
		token string
	}{
		{"пустой", ""},
		{"без точки", "nodot"},
		{"пустая подпись", "admin."},
		{"роль без подписи", "admin"},
		{"неизвестная роль", "superuser." + sign("superuser", testSecret)},
		{"подмена роли (подпись от user)", "admin." + userSig},
		{"подделка подписи", "user." + flipLast(userSig)},
		{"чужой секрет", "user." + sign("user", "other-secret")},
		{"лишние точки", "admin.sig.extra"},
		{"роль в другом регистре", "Admin." + sign("Admin", testSecret)},
		{"подпись неверной длины", "user.0"},
		{"подпись верной длины, неверная", "user." + strings.Repeat("0", 64)},
		{"перевод строки в подписи", "user." + userSig + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseToken(tc.token, testSecret); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("ParseToken(%q): ожидалась ErrInvalidToken, получено %v", tc.token, err)
			}
		})
	}
}

func TestIssueTokenUnknownRoleNotParseable(t *testing.T) {
	// IssueToken не валидирует роль; защита от эскалации — на стороне ParseToken
	token := IssueToken(Role("superuser"), testSecret)
	if _, err := ParseToken(token, testSecret); !errors.Is(err, ErrInvalidToken) {
		t.Error("токен с несуществующей ролью не должен парситься")
	}
}

func TestMiddlewareAuthorizationHeader(t *testing.T) {
	token := IssueToken(RoleAdmin, testSecret)
	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"валидный Bearer", "Bearer " + token, http.StatusOK},
		{"без заголовка", "", http.StatusUnauthorized},
		{"без схемы Bearer", token, http.StatusUnauthorized},
		{"нижний регистр bearer", "bearer " + token, http.StatusUnauthorized},
		{"двойной пробел", "Bearer  " + token, http.StatusUnauthorized},
		{"пустой токен", "Bearer ", http.StatusUnauthorized},
		{"схема Basic", "Basic " + token, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			Middleware(testSecret)(okHandler()).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("код = %d, ожидалось %d", rec.Code, tc.want)
			}
		})
	}
}

func TestMiddlewarePutsRoleInContext(t *testing.T) {
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

func TestMiddlewareIsolatesRolesPerRequest(t *testing.T) {
	mw := Middleware(testSecret)
	for _, role := range []Role{RoleUser, RoleAdmin} {
		var got Role
		inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got, _ = RoleFromContext(r.Context())
		})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+IssueToken(role, testSecret))
		mw(inner).ServeHTTP(httptest.NewRecorder(), req)
		if got != role {
			t.Errorf("роль = %q, ожидалось %q", got, role)
		}
	}
}

func TestRoleFromContextEmpty(t *testing.T) {
	if role, ok := RoleFromContext(context.Background()); ok || role != "" {
		t.Errorf("пустой контекст: role=%q ok=%v", role, ok)
	}
}
