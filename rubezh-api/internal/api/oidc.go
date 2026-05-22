package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/config"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// OIDCAuth — OIDC Relying Party (K.1): браузерный вход сотрудника через
// корпоративный IdP (Authorization Code + PKCE). После верификации ID-токена
// пользователь upsert'ится по email, выпускается токен «Рубежа» с реальным
// user_id, браузер возвращается на фронт. Доступ к LLM остаётся по ключам.
type OIDCAuth struct {
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
	cfg      config.OIDCConfig
	store    *storage.Storage
	secret   string
	logger   *slog.Logger
}

// NewOIDCAuth строит RP: запрашивает метаданные issuer'а (сеть). Ошибка →
// caller логирует и оставляет OIDC выключенным (dev-login продолжает работать).
func NewOIDCAuth(
	ctx context.Context, cfg config.OIDCConfig, store *storage.Storage,
	secret string, logger *slog.Logger,
) (*OIDCAuth, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	return &OIDCAuth{
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		cfg: cfg, store: store, secret: secret, logger: logger,
	}, nil
}

const _oidcCookieTTL = 10 * time.Minute

// login генерит state+nonce+PKCE (в httpOnly-cookie) и редиректит на IdP.
func (o *OIDCAuth) login(w http.ResponseWriter, r *http.Request) {
	state := randHex(16)
	nonce := randHex(16)
	verifier := oauth2.GenerateVerifier()
	secure := strings.HasPrefix(o.cfg.RedirectURL, "https://")
	o.setCookie(w, "oidc_state", state, secure)
	o.setCookie(w, "oidc_nonce", nonce, secure)
	o.setCookie(w, "oidc_verifier", verifier, secure)
	// K.2: CLI loopback-вход. cli_redirect разрешён ТОЛЬКО на loopback —
	// иначе токен можно было бы увести на чужой URL (open-redirect).
	if cli := r.URL.Query().Get("cli_redirect"); isLoopbackURL(cli) {
		o.setCookie(w, "oidc_cli_redirect", cli, secure)
	}
	authURL := o.oauth.AuthCodeURL(state,
		oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, authURL, http.StatusFound)
}

// callback проверяет state/PKCE/nonce, верифицирует ID-токен, upsert'ит юзера,
// выпускает токен «Рубежа» и редиректит на фронт (токен во фрагменте URL).
func (o *OIDCAuth) callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	if errMsg := q.Get("error"); errMsg != "" {
		o.fail(w, r, "IdP вернул ошибку авторизации")
		return
	}
	if !o.checkCookie(r, "oidc_state", q.Get("state")) {
		o.fail(w, r, "несовпадение state (возможна CSRF)")
		return
	}
	verifierC, err := r.Cookie("oidc_verifier")
	if err != nil {
		o.fail(w, r, "нет PKCE-verifier")
		return
	}
	tok, err := o.oauth.Exchange(ctx, q.Get("code"),
		oauth2.VerifierOption(verifierC.Value))
	if err != nil {
		o.logger.Warn("oidc: обмен кода не удался", "error", err)
		o.fail(w, r, "обмен кода на токен не удался")
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		o.fail(w, r, "в ответе нет id_token")
		return
	}
	idToken, err := o.verifier.Verify(ctx, rawID)
	if err != nil {
		o.logger.Warn("oidc: верификация id_token не удалась", "error", err)
		o.fail(w, r, "ID-токен не прошёл проверку")
		return
	}
	if !o.checkCookie(r, "oidc_nonce", idToken.Nonce) {
		o.fail(w, r, "несовпадение nonce")
		return
	}
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		o.fail(w, r, "не удалось прочитать claims")
		return
	}
	email, _ := claims["email"].(string)
	if email == "" {
		o.fail(w, r, "IdP не предоставил email")
		return
	}
	fullName, _ := claims["name"].(string)
	roleCode := o.mapRole(claims)

	userID, err := o.store.UpsertUserByEmail(ctx, email, fullName, roleCode)
	if err != nil {
		o.logger.Error("oidc: upsert пользователя не удался", "error", err)
		o.fail(w, r, "не удалось создать пользователя")
		return
	}
	token := auth.IssueTokenForUser(userID, auth.Role(roleCode), o.secret)
	o.logger.Info("oidc: вход выполнен", "role", roleCode) // без email/токена

	// K.2: CLI loopback — токен в query на 127.0.0.1 (fragment не доходит до
	// HTTP-сервера CLI). Иначе — web: токен во фрагменте (не в логах/Referer).
	if c, err := r.Cookie("oidc_cli_redirect"); err == nil && isLoopbackURL(c.Value) {
		target := c.Value + "?token=" + url.QueryEscape(token) +
			"&role=" + url.QueryEscape(roleCode)
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	target := strings.TrimRight(o.cfg.FrontendURL, "/") + "/login#token=" +
		url.QueryEscape(token) + "&role=" + url.QueryEscape(roleCode)
	http.Redirect(w, r, target, http.StatusFound)
}

// isLoopbackURL — true только для http(s)://127.0.0.1|localhost|[::1][:port][/path].
// Защита от увода токена: cli_redirect обязан указывать на loopback.
func isLoopbackURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// mapRole отображает значение claim'а роли на код роли проекта; least-privilege.
func (o *OIDCAuth) mapRole(claims map[string]any) string {
	if o.cfg.RoleClaim == "" || len(o.cfg.RoleMap) == 0 {
		return string(auth.RoleUser)
	}
	for _, v := range claimValues(claims[o.cfg.RoleClaim]) {
		if code, ok := o.cfg.RoleMap[v]; ok && auth.IsValidRole(auth.Role(code)) {
			return code
		}
	}
	return string(auth.RoleUser)
}

// claimValues приводит claim (строка или []any строк) к списку строк.
func claimValues(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func (o *OIDCAuth) fail(w http.ResponseWriter, r *http.Request, msg string) {
	if c, err := r.Cookie("oidc_cli_redirect"); err == nil && isLoopbackURL(c.Value) {
		http.Redirect(w, r, c.Value+"?error="+url.QueryEscape(msg),
			http.StatusFound)
		return
	}
	target := strings.TrimRight(o.cfg.FrontendURL, "/") +
		"/login#error=" + url.QueryEscape(msg)
	http.Redirect(w, r, target, http.StatusFound)
}

func (o *OIDCAuth) setCookie(w http.ResponseWriter, name, val string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: val, Path: "/api/auth/oidc",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
		MaxAge: int(_oidcCookieTTL.Seconds()),
	})
}

func (o *OIDCAuth) checkCookie(r *http.Request, name, expected string) bool {
	c, err := r.Cookie(name)
	return err == nil && expected != "" && c.Value == expected
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
