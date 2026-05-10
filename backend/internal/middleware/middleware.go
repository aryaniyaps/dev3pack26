package middleware

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dev3pack/ruby/backend/internal/config"
	"github.com/go-chi/cors"
)

func CORS(cfg *config.Config) func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowOriginFunc: func(r *http.Request, origin string) bool {
			return matchCORSOrigin(cfg, origin)
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	})
}

func matchCORSOrigin(cfg *config.Config, origin string) bool {
	if strings.TrimSpace(origin) == "" {
		return false
	}
	for _, rule := range cfg.CORSAllowedOrigins {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		if rule == origin {
			return true
		}
		// e.g. https://*.vercel.app → any https host ending with .vercel.app
		if strings.HasPrefix(rule, "https://*.") {
			suffix := strings.TrimPrefix(rule, "https://*.")
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				continue
			}
			if strings.EqualFold(u.Host, suffix) || strings.HasSuffix(strings.ToLower(u.Host), "."+strings.ToLower(suffix)) {
				return true
			}
		}
	}
	return false
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("Started %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		log.Printf("Completed %s %s in %v", r.Method, r.URL.Path, time.Since(start))
	})
}
