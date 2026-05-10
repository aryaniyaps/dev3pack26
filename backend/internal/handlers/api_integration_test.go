package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dev3pack/ruby/backend/internal/auth"
	"github.com/dev3pack/ruby/backend/internal/config"
	"github.com/dev3pack/ruby/backend/internal/db"
	"github.com/dev3pack/ruby/backend/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func integrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL (postgres DSN) for integration tests")
	}
	return dsn
}

func TestIntegration_ContributeCreditBeforeDeadline(t *testing.T) {
	database, err := db.Open(integrationDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := &config.Config{}
	h := NewHandler(database, cfg)
	fake := &fakeAuthService{
		principal: &auth.Principal{
			SessionID: "sess_int", UserID: "u", Provider: "phantom",
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		},
	}
	h.Auth = fake

	gid := "it_g_" + uuid.NewString()
	ctx := context.Background()
	dl := time.Now().UTC().Add(48 * time.Hour)
	g := &models.Group{
		ID: gid, Name: "Integration", CreatorWallet: "cw", ContributionAmt: 1000,
		MaxMembers: 10, CycleDeadline: &dl, ActiveCycle: 1,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if _, err := database.NewInsert().Model(g).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	mid := "it_m_" + uuid.NewString()
	m := &models.Member{
		ID: mid, GroupID: gid, WalletAddress: "WallIt_" + uuid.NewString()[:8],
		JoinedAt: time.Now().UTC(),
	}
	if _, err := database.NewInsert().Model(m).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(fake, testCookieConfig()))
		r.Post("/api/v1/groups/{groupID}/contribute", h.Contribute)
	})

	body := `{"member_id":"` + mid + `","wallet_address":"` + m.WalletAddress + `","amount":5000000,"cycle_number":1,"tx_signature":"tx_it_1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/groups/"+gid+"/contribute", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "ruby_session", Value: "valid-token", Path: "/"})
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("want 201 got %d: %s", rr.Code, rr.Body.String())
	}

	var mem models.Member
	if err := database.NewSelect().Model(&mem).Where("id = ?", mid).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if mem.CreditScore != 10 {
		t.Fatalf("expected credit 10, got %d", mem.CreditScore)
	}
}

func TestIntegration_HeliusContributeAndIdempotent(t *testing.T) {
	database, err := db.Open(integrationDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := &config.Config{RubyProgramID: "prog1"}
	h := NewHandler(database, cfg)

	gid := "it_hw_" + uuid.NewString()
	ctx := context.Background()
	dl := time.Now().UTC().Add(24 * time.Hour)
	g := &models.Group{
		ID: gid, Name: "H", CreatorWallet: "cw", ContributionAmt: 100,
		MaxMembers: 10, CycleDeadline: &dl, ActiveCycle: 1,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if _, err := database.NewInsert().Model(g).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	wallet := "HelWallet_" + uuid.NewString()[:8]
	mid := "it_hm_" + uuid.NewString()
	m := &models.Member{ID: mid, GroupID: gid, WalletAddress: wallet, JoinedAt: time.Now().UTC()}
	if _, err := database.NewInsert().Model(m).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	router.Post("/api/v1/webhooks/helius", h.HeliusWebhook)

	payload := `{"instruction":"contribute","group_id":"` + gid + `","member_id":"` + mid + `","wallet_address":"` + wallet + `","amount":1000000,"cycle_number":1,"signature":"unique_sig_helius_` + uuid.NewString() + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/helius", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("first webhook: %d %s", rr.Code, rr.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/helius", strings.NewReader(payload))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("second webhook: %d", rr2.Code)
	}

	n, err := database.NewSelect().Model((*models.Contribution)(nil)).Where("group_id = ?", gid).Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 contribution after duplicate webhook, got %d", n)
	}
}

func TestIntegration_GetMemberCreditScore(t *testing.T) {
	database, err := db.Open(integrationDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := &config.Config{}
	h := NewHandler(database, cfg)

	gid := "it_cr_" + uuid.NewString()
	ctx := context.Background()
	dl := time.Now().UTC().Add(time.Hour)
	g := &models.Group{
		ID: gid, Name: "C", CreatorWallet: "cw", ContributionAmt: 100,
		MaxMembers: 10, CycleDeadline: &dl, ActiveCycle: 1,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if _, err := database.NewInsert().Model(g).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	mid := "it_cred_" + uuid.NewString()
	m := &models.Member{
		ID: mid, GroupID: gid, WalletAddress: "WCredit", CreditScore: 42,
		JoinedAt: time.Now().UTC(),
	}
	if _, err := database.NewInsert().Model(m).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	r.Get("/api/v1/groups/{groupID}/members/{memberID}/credit-score", h.GetMemberCreditScore)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/"+gid+"/members/"+mid+"/credit-score", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if int(resp["credit_score"].(float64)) != 42 {
		t.Fatalf("credit_score want 42 got %v", resp["credit_score"])
	}
}

func TestIntegration_EndCycleSettlement(t *testing.T) {
	database, err := db.Open(integrationDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := &config.Config{}
	h := NewHandler(database, cfg)
	fake := &fakeAuthService{
		principal: &auth.Principal{
			SessionID: "cy_se", UserID: "u", Provider: "phantom",
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		},
	}
	h.Auth = fake

	gid := "it_cy_" + uuid.NewString()
	ctx := context.Background()
	dl := time.Now().UTC().Add(48 * time.Hour)
	g := &models.Group{
		ID: gid, Name: "Cycle", CreatorWallet: "cw", ContributionAmt: 100,
		MaxMembers: 10, VaultBalance: 10_000_000, CycleDeadline: &dl,
		ActiveCycle: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if _, err := database.NewInsert().Model(g).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	mA := &models.Member{ID: "it_A_" + uuid.NewString(), GroupID: gid, WalletAddress: "WA", JoinedAt: time.Now().UTC()}
	mB := &models.Member{ID: "it_B_" + uuid.NewString(), GroupID: gid, WalletAddress: "WB", JoinedAt: time.Now().UTC()}
	if _, err := database.NewInsert().Model(mA).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := database.NewInsert().Model(mB).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		mid    string
		amount int64
	}{
		{mA.ID, 6_000_000},
		{mB.ID, 4_000_000},
	} {
		co := &models.Contribution{
			GroupID: gid, MemberID: c.mid, Amount: c.amount, CycleNumber: 1,
			PaidAt: time.Now().UTC(),
		}
		if _, err := database.NewInsert().Model(co).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(fake, testCookieConfig()))
		r.Post("/api/v1/groups/{groupID}/cycle/end", h.EndGroupCycle)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/groups/"+gid+"/cycle/end", nil)
	req.AddCookie(&http.Cookie{Name: "ruby_session", Value: "valid-token", Path: "/"})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("end cycle: %d %s", rr.Code, rr.Body.String())
	}

	var settlements []models.CycleSettlement
	if err := database.NewSelect().Model(&settlements).Where("group_id = ?", gid).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if len(settlements) != 2 {
		t.Fatalf("want 2 settlements, got %d", len(settlements))
	}
}
