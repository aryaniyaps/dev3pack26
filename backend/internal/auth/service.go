package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dev3pack/ruby/backend/internal/config"
	"github.com/dev3pack/ruby/backend/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mr-tron/base58"
	"github.com/uptrace/bun"
)

type Service struct {
	cfg        *config.Config
	db         *bun.DB
	httpClient *http.Client
	nonces     map[string]nonceEntry
	mu         sync.Mutex
}

type nonceEntry struct {
	Nonce     string
	ExpiresAt time.Time
	Used      bool
}

type Principal struct {
	SessionID     string    `json:"session_id"`
	UserID        string    `json:"user_id"`
	WalletAddress string    `json:"wallet_address,omitempty"`
	Provider      string    `json:"provider"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type privyClaims struct {
	Sub           string `json:"sub"`
	Audience      any    `json:"aud"`
	WalletAddress string `json:"wallet_address,omitempty"`
	jwt.RegisteredClaims
}

func NewService(cfg *config.Config, db *bun.DB) *Service {
	return &Service{
		cfg: cfg,
		db:  db,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		nonces: map[string]nonceEntry{},
	}
}

func (s *Service) BeginPhantomLogin(walletAddress string) (string, string, error) {
	if strings.TrimSpace(walletAddress) == "" {
		return "", "", errors.New("wallet_address is required")
	}
	nonce, err := randomString(24)
	if err != nil {
		return "", "", err
	}
	message := fmt.Sprintf("Sign in to Ruby.\nDomain: %s\nWallet: %s\nNonce: %s", s.cfg.AuthDomain, walletAddress, nonce)
	s.mu.Lock()
	s.nonces[strings.ToLower(walletAddress)] = nonceEntry{
		Nonce:     nonce,
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	s.mu.Unlock()
	return nonce, message, nil
}

func (s *Service) VerifyPhantomSignature(walletAddress, nonce, message, signatureBase58 string) (*Principal, string, error) {
	if strings.TrimSpace(walletAddress) == "" || strings.TrimSpace(nonce) == "" || strings.TrimSpace(message) == "" || strings.TrimSpace(signatureBase58) == "" {
		return nil, "", errors.New("wallet_address, nonce, message and signature are required")
	}
	if err := validatePhantomMessage(message, s.cfg.AuthDomain, walletAddress, nonce); err != nil {
		return nil, "", err
	}

	key := strings.ToLower(walletAddress)
	s.mu.Lock()
	entry, ok := s.nonces[key]
	if ok && !entry.Used && time.Now().UTC().Before(entry.ExpiresAt) && entry.Nonce == nonce {
		entry.Used = true
		s.nonces[key] = entry
	} else {
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return nil, "", errors.New("invalid or expired nonce")
	}

	pub, err := base58.Decode(walletAddress)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, "", errors.New("invalid wallet_address")
	}
	sig, err := base58.Decode(signatureBase58)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, "", errors.New("invalid signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(message), sig) {
		return nil, "", errors.New("signature verification failed")
	}

	return s.issueSession(context.Background(), "phantom:"+walletAddress, walletAddress, "phantom")
}

func validatePhantomMessage(message, expectedDomain, expectedWallet, expectedNonce string) error {
	lines := strings.Split(strings.TrimSpace(message), "\n")
	if len(lines) < 4 {
		return errors.New("invalid message format")
	}
	if strings.TrimSpace(lines[0]) != "Sign in to Ruby." {
		return errors.New("invalid sign-in statement")
	}

	domainLine := fmt.Sprintf("Domain: %s", expectedDomain)
	walletLine := fmt.Sprintf("Wallet: %s", expectedWallet)
	nonceLine := fmt.Sprintf("Nonce: %s", expectedNonce)

	if strings.TrimSpace(lines[1]) != domainLine {
		return errors.New("message domain mismatch")
	}
	if strings.TrimSpace(lines[2]) != walletLine {
		return errors.New("message wallet mismatch")
	}
	if strings.TrimSpace(lines[3]) != nonceLine {
		return errors.New("message nonce mismatch")
	}
	return nil
}

func (s *Service) VerifyPrivyToken(privyToken string) (*Principal, string, error) {
	if strings.TrimSpace(privyToken) == "" {
		return nil, "", errors.New("privy_token is required")
	}
	if strings.TrimSpace(s.cfg.PrivyAppID) == "" {
		return nil, "", errors.New("PRIVY_APP_ID is required on backend")
	}

	alg, err := jwtHeaderAlg(privyToken)
	if err != nil {
		return nil, "", errors.New("invalid privy token")
	}

	var claims *privyClaims
	switch alg {
	case jwt.SigningMethodES256.Alg():
		claims, err = s.verifyPrivyES256Claims(privyToken)
	case jwt.SigningMethodRS256.Alg():
		if strings.TrimSpace(s.cfg.PrivyJWKSURL) == "" {
			return nil, "", errors.New("PRIVY_JWKS_URL is required for RS256 Privy tokens")
		}
		claims, err = s.verifyPrivyRS256Claims(privyToken)
	default:
		return nil, "", fmt.Errorf("unsupported privy jwt alg %q (use ES256 with PRIVY_VERIFICATION_KEY or RS256 with PRIVY_JWKS_URL)", alg)
	}
	if err != nil {
		return nil, "", err
	}
	if err := validatePrivyClaims(claims, s.cfg.PrivyAppID, s.cfg.PrivyIssuer); err != nil {
		return nil, "", err
	}

	return s.issueSession(context.Background(), claims.Sub, claims.WalletAddress, "privy")
}

func jwtHeaderAlg(tokenString string) (string, error) {
	parts := strings.Split(strings.TrimSpace(tokenString), ".")
	if len(parts) < 2 {
		return "", errors.New("malformed jwt")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	var hdr map[string]any
	if err := json.Unmarshal(raw, &hdr); err != nil {
		return "", err
	}
	alg, _ := hdr["alg"].(string)
	if alg == "" {
		return "", errors.New("missing alg")
	}
	return alg, nil
}

func (s *Service) verifyPrivyES256Claims(tokenString string) (*privyClaims, error) {
	pemStr := strings.TrimSpace(s.cfg.PrivyVerificationKey)
	if pemStr == "" {
		return nil, errors.New("PRIVY_VERIFICATION_KEY is required for Privy ES256 tokens (copy from Privy Dashboard → App settings → Verification key)")
	}
	key, err := jwt.ParseECPublicKeyFromPEM([]byte(pemStr))
	if err != nil {
		return nil, fmt.Errorf("invalid PRIVY_VERIFICATION_KEY: %w", err)
	}
	claims := &privyClaims{}
	_, err = jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodES256.Alg() {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return key, nil
	})
	if err != nil {
		return nil, errors.New("invalid privy token")
	}
	return claims, nil
}

func (s *Service) verifyPrivyRS256Claims(tokenString string) (*privyClaims, error) {
	claims := &privyClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if strings.TrimSpace(kid) == "" {
			return nil, errors.New("missing kid")
		}
		return s.fetchJWKPublicKey(kid)
	})
	if err != nil {
		return nil, errors.New("invalid privy token")
	}
	return claims, nil
}

func validatePrivyClaims(claims *privyClaims, appID, configuredIssuer string) error {
	if !privyIssuerOK(claims.Issuer, configuredIssuer) {
		return errors.New("invalid privy issuer")
	}
	if !containsAudience(claims.Audience, appID) {
		return errors.New("privy token audience mismatch")
	}
	if strings.TrimSpace(claims.Sub) == "" {
		return errors.New("missing subject in privy token")
	}
	return nil
}

func privyIssuerOK(iss, configured string) bool {
	if iss == "" {
		return false
	}
	if iss == configured {
		return true
	}
	// Current Privy access / identity JWTs commonly use issuer "privy.io".
	switch iss {
	case "privy.io", "https://privy.io":
		return true
	default:
		return false
	}
}

func (s *Service) ParseSessionToken(tokenString string) (*Principal, error) {
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return []byte(s.cfg.AuthJWTSecret), nil
	})
	if err != nil || !parsed.Valid {
		return nil, errors.New("invalid auth token")
	}

	sessionID, _ := claims["sid"].(string)
	userID, _ := claims["sub"].(string)
	provider, _ := claims["provider"].(string)
	wallet, _ := claims["wallet_address"].(string)
	expFloat, _ := claims["exp"].(float64)
	if sessionID == "" || userID == "" || provider == "" || expFloat == 0 {
		return nil, errors.New("malformed auth token")
	}

	var stored models.AuthSession
	if err := s.db.NewSelect().Model(&stored).Where("id = ?", sessionID).Scan(context.Background()); err != nil {
		return nil, errors.New("session not found")
	}
	if stored.RevokedAt != nil || time.Now().UTC().After(stored.ExpiresAt) {
		return nil, errors.New("session is not active")
	}

	return &Principal{
		SessionID:     sessionID,
		UserID:        userID,
		WalletAddress: wallet,
		Provider:      provider,
		ExpiresAt:     time.Unix(int64(expFloat), 0).UTC(),
	}, nil
}

func (s *Service) RevokeSession(sessionID string) error {
	now := time.Now().UTC()
	_, err := s.db.NewUpdate().
		Model((*models.AuthSession)(nil)).
		Set("revoked_at = ?", now).
		Where("id = ?", sessionID).
		Where("revoked_at IS NULL").
		Exec(context.Background())
	return err
}

func (s *Service) issueSession(ctx context.Context, userID, wallet, provider string) (*Principal, string, error) {
	sessionID, err := randomString(18)
	if err != nil {
		return nil, "", err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(s.cfg.AuthSessionTTLMin) * time.Minute)
	session := &models.AuthSession{
		ID:            sessionID,
		UserID:        userID,
		WalletAddress: wallet,
		Provider:      provider,
		CreatedAt:     now,
		ExpiresAt:     expiresAt,
	}
	if _, err := s.db.NewInsert().Model(session).Exec(ctx); err != nil {
		return nil, "", err
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sid":            sessionID,
		"sub":            userID,
		"provider":       provider,
		"wallet_address": wallet,
		"iat":            now.Unix(),
		"exp":            expiresAt.Unix(),
	})
	signed, err := token.SignedString([]byte(s.cfg.AuthJWTSecret))
	if err != nil {
		return nil, "", err
	}

	return &Principal{
		SessionID:     sessionID,
		UserID:        userID,
		WalletAddress: wallet,
		Provider:      provider,
		ExpiresAt:     expiresAt,
	}, signed, nil
}

func (s *Service) fetchJWKPublicKey(kid string) (*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, s.cfg.PrivyJWKSURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	for _, k := range payload.Keys {
		if k.Kid != kid || k.Kty != "RSA" {
			continue
		}
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, err
		}
		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, err
		}
		n := new(big.Int).SetBytes(nb)
		e := int(new(big.Int).SetBytes(eb).Int64())
		return &rsa.PublicKey{N: n, E: e}, nil
	}
	return nil, errors.New("kid not found in JWKS")
}

func containsAudience(raw any, expected string) bool {
	switch v := raw.(type) {
	case string:
		return v == expected
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == expected {
				return true
			}
		}
	case []string:
		for _, item := range v {
			if item == expected {
				return true
			}
		}
	}
	return false
}

func randomString(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
