package auth

import (
	"net/http"
	"strings"

	"github.com/dev3pack/ruby/backend/internal/config"
)

// ReadSessionToken returns the Ruby session JWT. Prefer Authorization: Bearer first so
// cross-site SPAs still work when third-party cookies are blocked; HttpOnly cookie remains best-effort.
func ReadSessionToken(r *http.Request, cfg *config.Config) string {
	authz := r.Header.Get("Authorization")
	if strings.HasPrefix(authz, "Bearer ") {
		if v := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer ")); v != "" {
			return v
		}
	}
	name := SessionCookieName(cfg)
	if c, err := r.Cookie(name); err == nil {
		if v := strings.TrimSpace(c.Value); v != "" {
			return v
		}
	}
	return ""
}

func SessionCookieName(cfg *config.Config) string {
	if cfg == nil {
		return "ruby_session"
	}
	if s := strings.TrimSpace(cfg.SessionCookieName); s != "" {
		return s
	}
	return "ruby_session"
}

// SetSessionCookie sets the HttpOnly session cookie on the API host.
func SetSessionCookie(w http.ResponseWriter, cfg *config.Config, jwt string) {
	if cfg == nil || strings.TrimSpace(jwt) == "" {
		return
	}
	c := &http.Cookie{
		Name:     SessionCookieName(cfg),
		Value:    jwt,
		Path:     "/",
		MaxAge:   int(cfg.AuthSessionTTLMin * 60),
		HttpOnly: true,
		Secure:   cfg.SessionCookieSecure,
		SameSite: cfg.SessionCookieSameSite,
	}
	http.SetCookie(w, c)
}

// ClearSessionCookie expires the session cookie (e.g. on logout).
func ClearSessionCookie(w http.ResponseWriter, cfg *config.Config) {
	if cfg == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName(cfg),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cfg.SessionCookieSecure,
		SameSite: cfg.SessionCookieSameSite,
	})
}
