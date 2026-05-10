package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/dev3pack/ruby/backend/internal/config"
)

type contextKey string

const principalContextKey contextKey = "auth_principal"

type TokenParser interface {
	ParseSessionToken(tokenString string) (*Principal, error)
}

func Middleware(svc TokenParser, cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ReadSessionToken(r, cfg)
			if strings.TrimSpace(token) == "" {
				writeAuthError(w, http.StatusUnauthorized, "missing session")
				return
			}
			principal, err := svc.ParseSessionToken(token)
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "invalid session token")
				return
			}
			ctx := context.WithValue(r.Context(), principalContextKey, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalContextKey).(*Principal)
	return p, ok
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + message + `"}`))
}

var ErrInvalidSessionToken = errors.New("invalid session token")
