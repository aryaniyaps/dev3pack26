package handlers

import (
	"net/http"

	"github.com/dev3pack/ruby/backend/internal/config"
)

func testCookieConfig() *config.Config {
	return &config.Config{
		AuthSessionTTLMin:     60,
		SessionCookieSameSite: http.SameSiteLaxMode,
		SessionCookieSecure:   false,
	}
}
