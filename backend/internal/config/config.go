package config

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port               string
	DatabaseURL        string
	AppEnv             string
	SolanaRPCURL       string
	HeliusAPIKey       string
	SendAIAPIKey       string
	AuthJWTSecret      string
	AuthSessionTTLMin  int64
	PrivyAppID             string
	PrivyIssuer            string
	PrivyJWKSURL           string
	PrivyVerificationKey   string // PEM EC public key for ES256 access / identity tokens (Privy Dashboard → App settings)
	// CORSAllowedOrigins lists exact browser origins allowed to call this API with credentials (comma-separated in env).
	CORSAllowedOrigins []string
	// SessionCookieName is the HttpOnly cookie storing the Ruby session JWT (default ruby_session).
	SessionCookieName string
	// SessionCookieSecure sets the Secure flag (required for SameSite=None; use true in production HTTPS).
	SessionCookieSecure bool
	// SessionCookieSameSite is Lax (default local), None (cross-site SPA + API), or Strict.
	SessionCookieSameSite http.SameSite
	AuthDomain         string
	BlinkBaseURL       string
	AgentAutoRun       bool
	AgentIntervalSec   int64
	RubyProgramID      string
	SwigProgramID      string
	Token2022ProgramID string
	AnchorIDLPath      string
	MinReserveLamports int64
	DeployPercent      int64
	KaminoAPYBps       int64
	JitoAPYBps         int64
}

func Load() *Config {
	_ = godotenv.Load() // Ignore error if .env doesn't exist

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	appEnv := envOrDefault("APP_ENV", "development")
	corsOrigins := parseCSVTrim(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if len(corsOrigins) == 0 {
		corsOrigins = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	}

	sessionSame := parseSameSite(envOrDefault("SESSION_COOKIE_SAMESITE", defaultSameSiteMode(appEnv)))
	sessionSecure := envBool("SESSION_COOKIE_SECURE", appEnv == "production")
	if sessionSame == http.SameSiteNoneMode {
		sessionSecure = true
	}

	return &Config{
		Port:               port,
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		AppEnv:             appEnv,
		SolanaRPCURL:       envOrDefault("SOLANA_RPC_URL", "https://api.devnet.solana.com"),
		HeliusAPIKey:       os.Getenv("HELIUS_API_KEY"),
		SendAIAPIKey:       os.Getenv("SENDAI_API_KEY"),
		AuthJWTSecret:      envOrDefault("AUTH_JWT_SECRET", "dev-change-me"),
		AuthSessionTTLMin:  envInt64("AUTH_SESSION_TTL_MINUTES", 60*24*7),
		PrivyAppID:           os.Getenv("PRIVY_APP_ID"),
		PrivyIssuer:          envOrDefault("PRIVY_ISSUER", "https://auth.privy.io"),
		PrivyJWKSURL:         os.Getenv("PRIVY_JWKS_URL"),
		// Privy dashboard / one-line env pastes often use literal \n; PEM must contain real newlines.
		PrivyVerificationKey: normalizePEMEnv(os.Getenv("PRIVY_VERIFICATION_KEY")),
		CORSAllowedOrigins:   corsOrigins,
		SessionCookieName:    os.Getenv("SESSION_COOKIE_NAME"),
		SessionCookieSecure:  sessionSecure,
		SessionCookieSameSite: sessionSame,
		AuthDomain:         envOrDefault("AUTH_DOMAIN", "localhost:3000"),
		BlinkBaseURL:       envOrDefault("BLINK_BASE_URL", "http://localhost:3000/blinks"),
		AgentAutoRun:       envBool("AGENT_AUTO_RUN", false),
		AgentIntervalSec:   envInt64("AGENT_INTERVAL_SECONDS", 86400),
		RubyProgramID:      envOrDefault("RUBY_PROGRAM_ID", "BsHiSX9NmkNMTYP8UcAY71aKVZwcCgA2Tea1tmgeQbdv"),
		SwigProgramID:      envOrDefault("SWIG_PROGRAM_ID", "Swig111111111111111111111111111111111111111"),
		Token2022ProgramID: envOrDefault("TOKEN_2022_PROGRAM_ID", "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"),
		AnchorIDLPath:      envOrDefault("ANCHOR_IDL_PATH", "../contracts/idl/ruby_protocol.json"),
		MinReserveLamports: envInt64("MIN_RESERVE_LAMPORTS", 100_000_000),
		DeployPercent:      envInt64("DEPLOY_PERCENT", 90),
		KaminoAPYBps:       envInt64("KAMINO_APY_BPS", 750),
		JitoAPYBps:         envInt64("JITO_APY_BPS", 620),
	}
}

func normalizePEMEnv(s string) string {
	if s == "" {
		return ""
	}
	return strings.ReplaceAll(s, "\\n", "\n")
}

func defaultSameSiteMode(appEnv string) string {
	if appEnv == "production" {
		return "none"
	}
	return "lax"
}

func parseSameSite(s string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

func parseCSVTrim(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	case "":
		return fallback
	default:
		return fallback
	}
}
