package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dev3pack/ruby/backend/internal/config"
)

func TestReadSessionTokenPrefersBearerOverCookie(t *testing.T) {
	cfg := &config.Config{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer from-header")
	req.AddCookie(&http.Cookie{Name: "ruby_session", Value: "from-cookie"})

	if got := ReadSessionToken(req, cfg); got != "from-header" {
		t.Fatalf("expected Bearer token, got %q", got)
	}
}

func TestReadSessionTokenFallsBackToCookie(t *testing.T) {
	cfg := &config.Config{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "ruby_session", Value: "from-cookie"})

	if got := ReadSessionToken(req, cfg); got != "from-cookie" {
		t.Fatalf("expected cookie token, got %q", got)
	}
}
