package handlers

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dev3pack/ruby/backend/internal/auth"
	"github.com/dev3pack/ruby/backend/internal/config"
	"github.com/dev3pack/ruby/backend/internal/models"
	"github.com/dev3pack/ruby/backend/internal/realtime"
	"github.com/dev3pack/ruby/backend/internal/treasury"
	"github.com/dev3pack/ruby/backend/internal/webhook"
	"github.com/dev3pack/ruby/backend/internal/web3"
	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"
)

type Handler struct {
	DB     *bun.DB
	Config *config.Config
	Web3   *web3.Client
	Anchor *web3.AnchorClient
	Auth   AuthService
	Hub    *realtime.Hub
}

type AuthService interface {
	BeginPhantomLogin(walletAddress string) (string, string, error)
	VerifyPhantomSignature(walletAddress, nonce, message, signatureBase58 string) (*auth.Principal, string, error)
	VerifyPrivyToken(privyToken string) (*auth.Principal, string, error)
	RevokeSession(sessionID string) error
	ParseSessionToken(tokenString string) (*auth.Principal, error)
}

func NewHandler(db *bun.DB, cfg *config.Config) *Handler {
	return &Handler{
		DB:     db,
		Config: cfg,
		Web3:   web3.NewClient(cfg.SolanaRPCURL),
		Anchor: web3.NewAnchorClient(web3.ProgramConfig{
			RubyProgramID:      cfg.RubyProgramID,
			SwigProgramID:      cfg.SwigProgramID,
			Token2022ProgramID: cfg.Token2022ProgramID,
			AnchorIDLPath:      cfg.AnchorIDLPath,
		}),
		Auth:   auth.NewService(cfg, db),
		Hub:    realtime.NewHub(),
	}
}

// New keeps compatibility with the legacy bootstrap in root main.go.
func New(db *bun.DB, cfg *config.Config) *Handler {
	return NewHandler(db, cfg)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (h *Handler) publish(eventType string, payload map[string]any) {
	if h.Hub == nil {
		return
	}
	h.Hub.Broadcast(eventType, payload)
}

func (h *Handler) publishContributionWS(groupID string, payload map[string]any) {
	if h.Hub == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["group_id"] = groupID
	h.Hub.BroadcastGroup(groupID, "contribution", payload)
}

func (h *Handler) publishYieldDeposit(groupID string, payload map[string]any) {
	if h.Hub == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["group_id"] = groupID
	h.Hub.BroadcastGroup(groupID, "yield_deposit", payload)
}

func (h *Handler) publishVoteCast(groupID string, payload map[string]any) {
	if h.Hub == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["group_id"] = groupID
	h.Hub.BroadcastGroup(groupID, "vote_cast", payload)
}

func (h *Handler) publishCycleEnded(groupID string, payload map[string]any) {
	if h.Hub == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["group_id"] = groupID
	h.Hub.BroadcastGroup(groupID, "cycle_ended", payload)
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "ruby-backend",
		"status":  "ok",
	})
}

// HealthReady returns 200 only when Postgres is reachable (post-migration).
func (h *Handler) HealthReady(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	ctx := r.Context()
	if err := h.DB.PingContext(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "ruby-backend",
		"status":  "ready",
		"db":      "ok",
	})
}

type authNonceRequest struct {
	WalletAddress string `json:"wallet_address"`
}

func (h *Handler) BeginPhantomAuth(w http.ResponseWriter, r *http.Request) {
	var req authNonceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	nonce, message, err := h.Auth.BeginPhantomLogin(req.WalletAddress)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"wallet_address": req.WalletAddress,
		"nonce":          nonce,
		"message":        message,
	})
}

type verifyPhantomRequest struct {
	WalletAddress string `json:"wallet_address"`
	Nonce         string `json:"nonce"`
	Message       string `json:"message"`
	Signature     string `json:"signature"`
}

func (h *Handler) VerifyPhantomAuth(w http.ResponseWriter, r *http.Request) {
	var req verifyPhantomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	principal, token, err := h.Auth.VerifyPhantomSignature(req.WalletAddress, req.Nonce, req.Message, req.Signature)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	auth.SetSessionCookie(w, h.Config, token)
	writeJSON(w, http.StatusOK, map[string]any{
		"principal": principal,
		"token":     token, // Ruby session JWT (same as cookie); use when cross-site cookies are dropped
	})
}

type verifyPrivyRequest struct {
	PrivyToken string `json:"privy_token"`
}

func (h *Handler) VerifyPrivyAuth(w http.ResponseWriter, r *http.Request) {
	var req verifyPrivyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.PrivyToken) == "" {
		writeError(w, http.StatusBadRequest, "privy_token is required")
		return
	}
	principal, token, err := h.Auth.VerifyPrivyToken(req.PrivyToken)
	if err != nil {
		status := http.StatusUnauthorized
		if privyVerifyIsServerMisconfig(err) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}
	auth.SetSessionCookie(w, h.Config, token)
	writeJSON(w, http.StatusOK, map[string]any{
		"principal": principal,
		"token":     token, // Ruby session JWT (same as cookie); use when cross-site cookies are dropped
	})
}

// privyVerifyIsServerMisconfig returns true when the failure is missing/wrong Privy
// verification setup on the server (not a bad end-user token).
func privyVerifyIsServerMisconfig(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "PRIVY_APP_ID is required on backend") ||
		strings.Contains(msg, "set PRIVY_JWKS_URL") ||
		strings.Contains(msg, "PRIVY_JWKS_URL is required for RS256") ||
		strings.Contains(msg, "invalid PRIVY_VERIFICATION_KEY")
}

func (h *Handler) AuthMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "auth context missing")
		return
	}
	writeJSON(w, http.StatusOK, principal)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "auth context missing")
		return
	}
	if err := h.Auth.RevokeSession(principal.SessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke session")
		return
	}
	auth.ClearSessionCookie(w, h.Config)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *Handler) GetGroups(w http.ResponseWriter, r *http.Request) {
	var groups []models.Group
	ctx := context.Background()

	if err := h.DB.NewSelect().
		Model(&groups).
		Relation("Members").
		Order("created_at DESC").
		Scan(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch groups")
		return
	}
	if groups == nil {
		groups = []models.Group{}
	}

	writeJSON(w, http.StatusOK, groups)
}

func (h *Handler) GetGroupYield(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupID")
	if strings.TrimSpace(groupID) == "" {
		writeError(w, http.StatusBadRequest, "groupID is required")
		return
	}

	ctx := context.Background()
	var events []models.YieldEvent
	if err := h.DB.NewSelect().
		Model(&events).
		Where("group_id = ?", groupID).
		Order("created_at DESC").
		Scan(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch group yield")
		return
	}

	var totalDeposited int64
	for _, e := range events {
		totalDeposited += e.AmountDeposited
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"group_id":        groupID,
		"event_count":     len(events),
		"total_deposited": totalDeposited,
		"events":          events,
	})
}

// ListGroups keeps compatibility with legacy root main.go routes.
func (h *Handler) ListGroups(w http.ResponseWriter, r *http.Request) {
	h.GetGroups(w, r)
}

type createGroupRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	CreatorWallet   string `json:"creator_wallet"`
	ContributionAmt int64  `json:"contribution_amt"`
	MaxMembers      int    `json:"max_members"`
	SwigVaultAddr   string `json:"swig_vault_addr"`
	OnChainPDA      string `json:"on_chain_pda"`
}

func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.CreatorWallet) == "" {
		writeError(w, http.StatusBadRequest, "id, name and creator_wallet are required")
		return
	}
	if req.ContributionAmt <= 0 {
		writeError(w, http.StatusBadRequest, "contribution_amt must be > 0")
		return
	}
	if req.MaxMembers == 0 {
		req.MaxMembers = 10
	}

	deadline := time.Now().UTC().Add(7 * 24 * time.Hour)
	group := &models.Group{
		ID:              req.ID,
		Name:            req.Name,
		CreatorWallet:   req.CreatorWallet,
		ContributionAmt: req.ContributionAmt,
		MaxMembers:      req.MaxMembers,
		SwigVaultAddr:   req.SwigVaultAddr,
		OnChainPDA:      req.OnChainPDA,
		ActiveCycle:     1,
		CycleDeadline:   &deadline,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}

	ctx := context.Background()
	if _, err := h.DB.NewInsert().Model(group).Exec(ctx); err != nil {
		writeError(w, http.StatusConflict, "failed to create group (maybe duplicate id)")
		return
	}

	writeJSON(w, http.StatusCreated, group)
	h.publish("group.created", map[string]any{"group_id": group.ID, "creator_wallet": group.CreatorWallet})
}

type contributeRequest struct {
	MemberID      string `json:"member_id"`
	WalletAddress string `json:"wallet_address"`
	Amount        int64  `json:"amount"`
	CycleNumber   int    `json:"cycle_number"`
	TxSignature   string `json:"tx_signature"`
	OnTime        *bool  `json:"on_time,omitempty"` // used only when group has no cycle_deadline
}

type joinGroupRequest struct {
	MemberID      string `json:"member_id"`
	WalletAddress string `json:"wallet_address"`
	InviteCode    string `json:"invite_code,omitempty"`
	ReferrerID    string `json:"referrer_member_id,omitempty"`
}

type createInviteLinkRequest struct {
	ReferrerID string `json:"referrer_member_id"`
}

func buildInviteCode(groupID, referrerID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(groupID + ":" + referrerID))
}

func parseInviteCode(code string) (groupID string, referrerID string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(code))
	if err != nil {
		return "", "", errors.New("invalid invite_code")
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", errors.New("invalid invite_code")
	}
	return parts[0], parts[1], nil
}

func (h *Handler) CreateGroupInviteLink(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupID")
	if strings.TrimSpace(groupID) == "" {
		writeError(w, http.StatusBadRequest, "groupID is required")
		return
	}

	var req createInviteLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.ReferrerID) == "" {
		writeError(w, http.StatusBadRequest, "referrer_member_id is required")
		return
	}

	ctx := context.Background()
	var referrer models.Member
	if err := h.DB.NewSelect().
		Model(&referrer).
		Where("id = ?", req.ReferrerID).
		Where("group_id = ?", groupID).
		Scan(ctx); err != nil {
		writeError(w, http.StatusNotFound, "referrer member not found in group")
		return
	}

	frontendBase := strings.TrimSpace(h.Config.AuthDomain)
	if frontendBase == "" {
		frontendBase = "localhost:3000"
	}
	if !strings.HasPrefix(frontendBase, "http://") && !strings.HasPrefix(frontendBase, "https://") {
		frontendBase = "http://" + frontendBase
	}
	inviteCode := buildInviteCode(groupID, req.ReferrerID)
	inviteURL := fmt.Sprintf("%s/?group=%s&invite=%s",
		strings.TrimSuffix(frontendBase, "/"),
		url.QueryEscape(groupID),
		url.QueryEscape(inviteCode),
	)

	writeJSON(w, http.StatusOK, map[string]string{
		"group_id":           groupID,
		"referrer_member_id": req.ReferrerID,
		"invite_code":        inviteCode,
		"invite_url":         inviteURL,
	})
}

func (h *Handler) JoinGroup(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupID")
	if strings.TrimSpace(groupID) == "" {
		writeError(w, http.StatusBadRequest, "groupID is required")
		return
	}

	var req joinGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.MemberID) == "" || strings.TrimSpace(req.WalletAddress) == "" {
		writeError(w, http.StatusBadRequest, "member_id and wallet_address are required")
		return
	}
	req.ReferrerID = strings.TrimSpace(req.ReferrerID)
	req.InviteCode = strings.TrimSpace(req.InviteCode)
	if req.ReferrerID != "" && req.InviteCode != "" {
		writeError(w, http.StatusBadRequest, "provide either invite_code or referrer_member_id, not both")
		return
	}

	ctx := context.Background()
	var group models.Group
	if err := h.DB.NewSelect().Model(&group).Where("id = ?", groupID).Scan(ctx); err != nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}

	existing, err := h.DB.NewSelect().
		Model((*models.Member)(nil)).
		Where("group_id = ?", groupID).
		Count(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count members")
		return
	}
	if existing >= group.MaxMembers {
		writeError(w, http.StatusConflict, "group member limit reached")
		return
	}

	existingWallets, err := h.DB.NewSelect().
		Model((*models.Member)(nil)).
		Where("group_id = ?", groupID).
		Where("lower(wallet_address) = lower(?)", req.WalletAddress).
		Count(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check existing wallet membership")
		return
	}
	if existingWallets > 0 {
		writeError(w, http.StatusConflict, "wallet already joined this group")
		return
	}

	referrerID := req.ReferrerID
	referralSource := "direct_referrer"
	if req.InviteCode != "" {
		decodedGroupID, decodedReferrerID, err := parseInviteCode(req.InviteCode)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if decodedGroupID != groupID {
			writeError(w, http.StatusBadRequest, "invite_code does not belong to this group")
			return
		}
		referrerID = decodedReferrerID
		referralSource = "invite_link"
	}

	member := &models.Member{
		ID:            req.MemberID,
		GroupID:       groupID,
		WalletAddress: req.WalletAddress,
		JoinedAt:      time.Now().UTC(),
	}

	var referrer *models.Member
	if referrerID != "" {
		ref := &models.Member{}
		if err := h.DB.NewSelect().
			Model(ref).
			Where("id = ?", referrerID).
			Where("group_id = ?", groupID).
			Scan(ctx); err != nil {
			writeError(w, http.StatusBadRequest, "invalid referrer_member_id")
			return
		}

		if strings.EqualFold(ref.ID, member.ID) || strings.EqualFold(ref.WalletAddress, member.WalletAddress) {
			writeError(w, http.StatusBadRequest, "self-referrals are not allowed")
			return
		}
		referrer = ref
	}

	referralApplied := false
	if _, err := h.DB.NewInsert().Model(member).Exec(ctx); err != nil {
		writeError(w, http.StatusConflict, "failed to join group (member may already exist)")
		return
	}

	if referrer != nil {
		existingAttribution, err := h.DB.NewSelect().
			Model((*models.ReferralAttribution)(nil)).
			Where("group_id = ?", groupID).
			Where("invited_member_id = ?", member.ID).
			Count(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify referral attribution")
			return
		}
		if existingAttribution == 0 {
			referrer.CreditScore += 1
			if _, err := h.DB.NewUpdate().
				Model(referrer).
				Column("credit_score").
				WherePK().
				Exec(ctx); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to apply referrer credit")
				return
			}
			attribution := &models.ReferralAttribution{
				GroupID:          groupID,
				InvitedMemberID:  member.ID,
				InvitedWallet:    member.WalletAddress,
				ReferrerMemberID: referrer.ID,
				ReferrerWallet:   referrer.WalletAddress,
				Source:           referralSource,
				CreatedAt:        time.Now().UTC(),
			}
			if _, err := h.DB.NewInsert().Model(attribution).Exec(ctx); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to save referral attribution")
				return
			}
			referralApplied = true
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"member":           member,
		"referral_applied": referralApplied,
		"referrer_member":  referrerID,
	})
	h.publish("group.joined", map[string]any{
		"group_id":           groupID,
		"member_id":          member.ID,
		"wallet_address":     member.WalletAddress,
		"referral_applied":   referralApplied,
		"referrer_member_id": referrerID,
	})
}

func (h *Handler) Contribute(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupID")
	if strings.TrimSpace(groupID) == "" {
		writeError(w, http.StatusBadRequest, "groupID is required")
		return
	}

	var req contributeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.MemberID) == "" || strings.TrimSpace(req.WalletAddress) == "" {
		writeError(w, http.StatusBadRequest, "member_id and wallet_address are required")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be > 0")
		return
	}
	if req.CycleNumber <= 0 {
		writeError(w, http.StatusBadRequest, "cycle_number must be > 0")
		return
	}

	ctx := context.Background()
	var group models.Group
	if err := h.DB.NewSelect().Model(&group).Where("id = ?", groupID).Scan(ctx); err != nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}

	var member models.Member
	err := h.DB.NewSelect().Model(&member).
		Where("id = ?", req.MemberID).
		Where("group_id = ?", groupID).
		Scan(ctx)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to fetch member")
			return
		}
		member = models.Member{
			ID:            req.MemberID,
			GroupID:       groupID,
			WalletAddress: req.WalletAddress,
			JoinedAt:      time.Now().UTC(),
		}
		if _, err := h.DB.NewInsert().Model(&member).Exec(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create member")
			return
		}
	}

	paidAt := time.Now().UTC()
	contribution := &models.Contribution{
		GroupID:     groupID,
		MemberID:    req.MemberID,
		Amount:      req.Amount,
		CycleNumber: req.CycleNumber,
		TxSignature: req.TxSignature,
		PaidAt:      paidAt,
	}
	if _, err := h.DB.NewInsert().Model(contribution).Exec(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save contribution")
		return
	}

	creditDelta, creditReason := CreditDeltaForContribution(paidAt, group.CycleDeadline, req.OnTime)

	member.TotalContributed += req.Amount
	member.CreditScore = applyCreditDelta(member.CreditScore, creditDelta)
	if _, err := h.DB.NewUpdate().
		Model(&member).
		Column("total_contributed", "credit_score").
		WherePK().
		Exec(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update member totals")
		return
	}

	group.VaultBalance += req.Amount
	if req.CycleNumber > group.CycleCount {
		group.CycleCount = req.CycleNumber
	}
	group.UpdatedAt = time.Now().UTC()
	if _, err := h.DB.NewUpdate().
		Model(&group).
		Column("vault_balance", "cycle_count", "updated_at").
		WherePK().
		Exec(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update group")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"group_id":       groupID,
		"member_id":      member.ID,
		"amount":         req.Amount,
		"vault_balance":  group.VaultBalance,
		"credit_score":   member.CreditScore,
		"credit_delta":   creditDelta,
		"credit_reason":  creditReason,
		"cycle_number":   group.CycleCount,
		"cycle_deadline": group.CycleDeadline,
	})
	h.publishContributionWS(groupID, map[string]any{
		"group_id":       groupID,
		"member_id":      member.ID,
		"amount":         req.Amount,
		"vault_balance":  group.VaultBalance,
		"credit_delta":   creditDelta,
		"credit_reason":  creditReason,
		"credit_score":   member.CreditScore,
	})
}

type createYieldEventRequest struct {
	AmountDeposited int64      `json:"amount_deposited"`
	Protocol        string     `json:"protocol"`
	APY             float64    `json:"apy"`
	TxSignature     string     `json:"tx_signature"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
}

func (h *Handler) CreateYieldEvent(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupID")
	if strings.TrimSpace(groupID) == "" {
		writeError(w, http.StatusBadRequest, "groupID is required")
		return
	}

	var req createYieldEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.AmountDeposited <= 0 {
		writeError(w, http.StatusBadRequest, "amount_deposited must be > 0")
		return
	}
	if strings.TrimSpace(req.Protocol) == "" {
		writeError(w, http.StatusBadRequest, "protocol is required")
		return
	}

	createdAt := time.Now().UTC()
	if req.CreatedAt != nil {
		createdAt = req.CreatedAt.UTC()
	}

	event := &models.YieldEvent{
		GroupID:         groupID,
		AmountDeposited: req.AmountDeposited,
		Protocol:        strings.ToLower(req.Protocol),
		APY:             req.APY,
		TxSignature:     req.TxSignature,
		CreatedAt:       createdAt,
	}

	ctx := context.Background()
	if _, err := h.DB.NewInsert().Model(event).Exec(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create yield event")
		return
	}

	writeJSON(w, http.StatusCreated, event)
	h.publishYieldDeposit(groupID, map[string]any{
		"group_id":         groupID,
		"amount_deposited": event.AmountDeposited,
		"protocol":         event.Protocol,
		"apy":              event.APY,
	})
}

type swigConfigRequest struct {
	SwigVaultAddr string `json:"swig_vault_addr"`
	SwigQuorum    int    `json:"swig_quorum"`
}

func (h *Handler) ConfigureSwig(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupID")
	if strings.TrimSpace(groupID) == "" {
		writeError(w, http.StatusBadRequest, "groupID is required")
		return
	}

	var req swigConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.SwigVaultAddr) == "" || req.SwigQuorum <= 0 {
		writeError(w, http.StatusBadRequest, "swig_vault_addr and positive swig_quorum are required")
		return
	}

	ctx := context.Background()
	if _, err := h.DB.NewUpdate().
		Model((*models.Group)(nil)).
		Set("swig_vault_addr = ?", req.SwigVaultAddr).
		Set("swig_quorum = ?", req.SwigQuorum).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", groupID).
		Exec(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store swig config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"group_id":        groupID,
		"swig_vault_addr": req.SwigVaultAddr,
		"swig_quorum":     req.SwigQuorum,
	})
}

type tokenConfigRequest struct {
	GroupTokenMint  string `json:"group_token_mint"`
	TransferHookPID string `json:"transfer_hook_pid"`
}

func (h *Handler) ConfigureToken2022(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupID")
	if strings.TrimSpace(groupID) == "" {
		writeError(w, http.StatusBadRequest, "groupID is required")
		return
	}

	var req tokenConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.GroupTokenMint) == "" || strings.TrimSpace(req.TransferHookPID) == "" {
		writeError(w, http.StatusBadRequest, "group_token_mint and transfer_hook_pid are required")
		return
	}

	ctx := context.Background()
	if _, err := h.DB.NewUpdate().
		Model((*models.Group)(nil)).
		Set("group_token_mint = ?", req.GroupTokenMint).
		Set("transfer_hook_pid = ?", req.TransferHookPID).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", groupID).
		Exec(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store token config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"group_id":          groupID,
		"group_token_mint":  req.GroupTokenMint,
		"transfer_hook_pid": req.TransferHookPID,
	})
}

func (h *Handler) GetGroupExplorerLinks(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupID")
	if strings.TrimSpace(groupID) == "" {
		writeError(w, http.StatusBadRequest, "groupID is required")
		return
	}

	ctx := context.Background()
	var group models.Group
	if err := h.DB.NewSelect().Model(&group).Where("id = ?", groupID).Scan(ctx); err != nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"group_pda":     group.OnChainPDA,
		"group_pda_url": fmt.Sprintf("https://explorer.solana.com/address/%s?cluster=devnet", group.OnChainPDA),
		"vault_addr":    group.SwigVaultAddr,
		"vault_url":     fmt.Sprintf("https://explorer.solana.com/address/%s?cluster=devnet", group.SwigVaultAddr),
		"token_mint":    group.GroupTokenMint,
		"token_url":     fmt.Sprintf("https://explorer.solana.com/address/%s?cluster=devnet", group.GroupTokenMint),
	})
}

type runAgentRequest struct {
	GroupID string `json:"group_id,omitempty"`
	DryRun  bool   `json:"dry_run"`
}

func (h *Handler) runTreasuryAgentInternal(groupID string, dryRun bool) ([]*treasury.AgentResult, error) {
	ctx := context.Background()
	var groups []models.Group
	query := h.DB.NewSelect().Model(&groups).Order("created_at DESC")
	if strings.TrimSpace(groupID) != "" {
		query = query.Where("id = ?", groupID)
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return []*treasury.AgentResult{}, nil
	}

	cfg := treasury.AgentConfig{
		MinReserveLamports: h.Config.MinReserveLamports,
		DeployPercent:      h.Config.DeployPercent,
		KaminoAPYBps:       h.Config.KaminoAPYBps,
		JitoAPYBps:         h.Config.JitoAPYBps,
	}

	results := make([]*treasury.AgentResult, 0, len(groups))
	for _, group := range groups {
		result, err := treasury.RunOnce(ctx, h.DB, cfg, group, dryRun)
		if err != nil {
			results = append(results, &treasury.AgentResult{
				GroupID: group.ID,
				Status:  "failed",
				Reason:  err.Error(),
			})
			continue
		}
		results = append(results, result)
		if result.Status == "executed" && result.DepositedLamports > 0 {
			h.publishYieldDeposit(result.GroupID, map[string]any{
				"group_id":           result.GroupID,
				"status":             result.Status,
				"selected_protocol":  result.SelectedProtocol,
				"selected_apy":       result.SelectedAPY,
				"deposited_lamports": result.DepositedLamports,
				"source":             "treasury_agent",
			})
		}
	}
	return results, nil
}

func (h *Handler) RunTreasuryAgent(w http.ResponseWriter, r *http.Request) {
	var req runAgentRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	results, err := h.runTreasuryAgentInternal(req.GroupID, req.DryRun)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load groups")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"run_at":  time.Now().UTC(),
		"dry_run": req.DryRun,
		"results": results,
	})
}

func (h *Handler) Run() error {
	_, err := h.runTreasuryAgentInternal("", false)
	return err
}

func (h *Handler) ListChainEvents(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	limit := 50
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	var events []models.ChainEvent
	if err := h.DB.NewSelect().
		Model(&events).
		Order("created_at DESC").
		Limit(limit).
		Scan(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chain events")
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (h *Handler) SolanaWalletBalance(w http.ResponseWriter, r *http.Request) {
	address := strings.TrimSpace(r.URL.Query().Get("address"))
	if address == "" {
		writeError(w, http.StatusBadRequest, "address query param is required")
		return
	}

	balance, err := h.Web3.GetBalanceLamports(r.Context(), address)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch balance from Solana RPC")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"address":        address,
		"lamports":       balance,
		"sol":            float64(balance) / 1_000_000_000,
		"solana_rpc_url": h.Config.SolanaRPCURL,
	})
}

type heliusWebhookPayload struct {
	Type      string `json:"type"`
	Signature string `json:"signature"`
	GroupID   string `json:"group_id"`
}

func (h *Handler) HeliusWebhook(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.Config.HeliusAPIKey) != "" {
		got := strings.TrimSpace(r.Header.Get("X-Helius-Api-Key"))
		if got != h.Config.HeliusAPIKey {
			writeError(w, http.StatusUnauthorized, "invalid helius api key")
			return
		}
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	meta := heliusWebhookPayload{
		Type:      "unknown",
		Signature: "",
		GroupID:   "",
	}
	if v, ok := payload["type"].(string); ok {
		meta.Type = v
	}
	if v, ok := payload["signature"].(string); ok {
		meta.Signature = v
	}
	if v, ok := payload["group_id"].(string); ok {
		meta.GroupID = v
	}

	event := &models.ChainEvent{
		GroupID:    meta.GroupID,
		EventType:  meta.Type,
		Signature:  meta.Signature,
		RawPayload: string(rawBody),
		CreatedAt:  time.Now().UTC(),
	}
	if _, err := h.DB.NewInsert().Model(event).Exec(context.Background()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist helius webhook")
		return
	}

	ctx := context.Background()
	ops, err := webhook.ParseHeliusJSON(rawBody, h.Config.RubyProgramID)
	if err != nil {
		log.Printf("helius: parse ops: %v", err)
	}
	for _, op := range ops {
		switch op.Kind {
		case "contribute":
			mid := strings.TrimSpace(op.MemberID)
			if mid == "" {
				mid = op.WalletAddress
			}
			if err := h.applyWebhookContribution(ctx, webhookContributionOp{
				GroupID:       op.GroupID,
				MemberID:      mid,
				WalletAddress: op.WalletAddress,
				Amount:        op.Amount,
				CycleNumber:   op.CycleNumber,
				Signature:     op.Signature,
			}); err != nil {
				log.Printf("helius: apply contribute: %v", err)
			}
		case "yield_event":
			if err := h.applyWebhookYield(ctx, webhookYieldOp{
				GroupID:         op.GroupID,
				AmountDeposited: op.AmountDeposited,
				Protocol:        op.Protocol,
				APY:             op.APY,
				Signature:       op.Signature,
			}); err != nil {
				log.Printf("helius: apply yield_event: %v", err)
			}
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	h.publish("helius.webhook", map[string]any{
		"group_id":    meta.GroupID,
		"event_type":  meta.Type,
		"signature":   meta.Signature,
		"parsed_ops":  len(ops),
	})
}
