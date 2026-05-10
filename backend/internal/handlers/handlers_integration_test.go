package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dev3pack/ruby/backend/internal/auth"
	"github.com/dev3pack/ruby/backend/internal/config"
	"github.com/dev3pack/ruby/backend/internal/web3"
	"github.com/go-chi/chi/v5"
)

func TestHealthEndpoint(t *testing.T) {
	h := &Handler{}
	r := chi.NewRouter()
	r.Get("/health", h.Health)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if payload["status"] != "ok" || payload["service"] != "ruby-backend" {
		t.Fatalf("unexpected health payload: %+v", payload)
	}
}

func TestHealthReadyWithoutDB(t *testing.T) {
	h := &Handler{}
	r := chi.NewRouter()
	r.Get("/health/ready", h.HealthReady)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without DB, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSolanaWalletBalanceEndpoint(t *testing.T) {
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"context":{"slot":1},"value":2000000000}}`))
	}))
	defer rpc.Close()

	h := &Handler{
		Config: &config.Config{SolanaRPCURL: rpc.URL},
		Web3:   web3.NewClient(rpc.URL),
	}

	router := chi.NewRouter()
	router.Get("/api/v1/web3/balance", h.SolanaWalletBalance)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web3/balance?address=wallet111", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if payload["address"] != "wallet111" {
		t.Fatalf("unexpected address: %+v", payload)
	}
}

func TestSolanaWalletBalanceEndpointMissingAddress(t *testing.T) {
	h := &Handler{
		Config: &config.Config{SolanaRPCURL: "http://example.invalid"},
		Web3:   web3.NewClient("http://example.invalid"),
	}

	router := chi.NewRouter()
	router.Get("/api/v1/web3/balance", h.SolanaWalletBalance)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web3/balance", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

type fakeAuthService struct {
	principal *auth.Principal
	revokedID string
}

func (f *fakeAuthService) BeginPhantomLogin(walletAddress string) (string, string, error) {
	return "", "", nil
}
func (f *fakeAuthService) VerifyPhantomSignature(walletAddress, nonce, message, signatureBase58 string) (*auth.Principal, string, error) {
	return nil, "", nil
}
func (f *fakeAuthService) VerifyPrivyToken(privyToken string) (*auth.Principal, string, error) {
	return nil, "", nil
}
func (f *fakeAuthService) RevokeSession(sessionID string) error {
	f.revokedID = sessionID
	return nil
}
func (f *fakeAuthService) ParseSessionToken(tokenString string) (*auth.Principal, error) {
	if tokenString != "valid-token" {
		return nil, auth.ErrInvalidSessionToken
	}
	return f.principal, nil
}

func TestAuthMeAndLogoutProtectedEndpoints(t *testing.T) {
	fake := &fakeAuthService{
		principal: &auth.Principal{
			SessionID:     "sess_123",
			UserID:        "user_123",
			WalletAddress: "wallet_abc",
			Provider:      "phantom",
			ExpiresAt:     time.Now().UTC().Add(30 * time.Minute),
		},
	}
	h := &Handler{Auth: fake, Config: testCookieConfig()}

	router := chi.NewRouter()
	router.Route("/api/v1/auth", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(fake, testCookieConfig()))
			r.Get("/me", h.AuthMe)
			r.Post("/logout", h.Logout)
		})
	})

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq = meReq.WithContext(context.Background())
	meReq.AddCookie(&http.Cookie{Name: "ruby_session", Value: "valid-token", Path: "/"})
	meRR := httptest.NewRecorder()
	router.ServeHTTP(meRR, meReq)
	if meRR.Code != http.StatusOK {
		t.Fatalf("expected 200 on me, got %d", meRR.Code)
	}

	var mePayload map[string]any
	if err := json.Unmarshal(meRR.Body.Bytes(), &mePayload); err != nil {
		t.Fatalf("failed to parse me payload: %v", err)
	}
	if mePayload["session_id"] != "sess_123" {
		t.Fatalf("unexpected me payload: %+v", mePayload)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq = logoutReq.WithContext(context.Background())
	logoutReq.AddCookie(&http.Cookie{Name: "ruby_session", Value: "valid-token", Path: "/"})
	logoutRR := httptest.NewRecorder()
	router.ServeHTTP(logoutRR, logoutReq)
	if logoutRR.Code != http.StatusOK {
		t.Fatalf("expected 200 on logout, got %d", logoutRR.Code)
	}
	if fake.revokedID != "sess_123" {
		t.Fatalf("expected revoked session sess_123, got %s", fake.revokedID)
	}
}

func TestInviteCodeRoundTrip(t *testing.T) {
	groupID := "group_123"
	referrerID := "member_456"

	code := buildInviteCode(groupID, referrerID)
	gotGroup, gotReferrer, err := parseInviteCode(code)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if gotGroup != groupID || gotReferrer != referrerID {
		t.Fatalf("unexpected decoded values: group=%s referrer=%s", gotGroup, gotReferrer)
	}
}

func TestProtectedRouteRejectsMissingBearer(t *testing.T) {
	fake := &fakeAuthService{
		principal: &auth.Principal{
			SessionID: "sess_123",
			UserID:    "user_123",
			Provider:  "phantom",
			ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
		},
	}
	h := &Handler{Auth: fake, Config: testCookieConfig()}

	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(auth.Middleware(fake, testCookieConfig()))
		r.Post("/api/v1/groups", h.CreateGroup)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestHeliusWebhookRejectsInvalidAPIKey(t *testing.T) {
	h := &Handler{Config: &config.Config{HeliusAPIKey: "expected-key"}}
	router := chi.NewRouter()
	router.Post("/api/v1/webhooks/helius", h.HeliusWebhook)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/helius", nil)
	req.Header.Set("X-Helius-Api-Key", "wrong-key")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestOnchainConfigEndpoint(t *testing.T) {
	h := &Handler{
		Config: &config.Config{
			RubyProgramID:      "ruby111",
			SwigProgramID:      "swig111",
			Token2022ProgramID: "token111",
			AnchorIDLPath:      "missing.json",
		},
		Anchor: web3.NewAnchorClient(web3.ProgramConfig{
			RubyProgramID:      "ruby111",
			SwigProgramID:      "swig111",
			Token2022ProgramID: "token111",
			AnchorIDLPath:      "missing.json",
		}),
	}
	router := chi.NewRouter()
	router.Get("/api/v1/onchain/config", h.OnchainConfig)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/onchain/config", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestBuildTxEndpoint(t *testing.T) {
	h := &Handler{
		Anchor: web3.NewAnchorClient(web3.ProgramConfig{
			RubyProgramID:      "ruby111",
			SwigProgramID:      "swig111",
			Token2022ProgramID: "token111",
			AnchorIDLPath:      "missing.json",
		}),
	}
	router := chi.NewRouter()
	router.Post("/api/v1/tx/build/{action}", h.BuildTx)

	body := strings.NewReader(`{"group_id":"g1","amount":1000}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tx/build/create-group", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestBuildTxEndpointRejectsUnsupportedAction(t *testing.T) {
	h := &Handler{
		Anchor: web3.NewAnchorClient(web3.ProgramConfig{
			RubyProgramID:      "ruby111",
			SwigProgramID:      "swig111",
			Token2022ProgramID: "token111",
			AnchorIDLPath:      "missing.json",
		}),
	}
	router := chi.NewRouter()
	router.Post("/api/v1/tx/build/{action}", h.BuildTx)

	body := strings.NewReader(`{"group_id":"g1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tx/build/not-a-real-action", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHeliusWebhookAcceptsValidAPIKey(t *testing.T) {
	h := &Handler{
		Config: &config.Config{HeliusAPIKey: "expected-key"},
	}
	router := chi.NewRouter()
	router.Post("/api/v1/webhooks/helius", h.HeliusWebhook)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/helius", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Helius-Api-Key", "expected-key")
	rr := httptest.NewRecorder()
	// with valid key, request reaches JSON validation and fails with 400.
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 after passing API key check, got %d", rr.Code)
	}
}
