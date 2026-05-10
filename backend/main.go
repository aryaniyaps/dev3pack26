package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dev3pack/ruby/backend/internal/auth"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/dev3pack/ruby/backend/internal/config"
	"github.com/dev3pack/ruby/backend/internal/db"
	"github.com/dev3pack/ruby/backend/internal/handlers"
	"github.com/dev3pack/ruby/backend/internal/middleware"
	"github.com/dev3pack/ruby/backend/internal/treasury"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "migrate" {
		runMigrateCLI()
		return
	}
	runServer()
}

func runMigrateCLI() {
	cfg := config.Load()
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		log.Fatal("migrate: DATABASE_URL is required")
	}
	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("migrate: %v", err)
	}
	defer database.Close()
	log.Println("migrate: database migrations completed successfully")
}

func runServer() {
	cfg := config.Load()

	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database setup failed (migrations): %v", err)
	}
	defer database.Close()
	log.Println("database migrations completed successfully")

	h := handlers.New(database, cfg)

	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS(cfg))

	// Health check (no version prefix — used by Render health probe)
	r.Get("/health", h.Health)
	r.Get("/health/ready", h.HealthReady)
	r.Get("/api/v1/ws", h.EventsWebSocket)

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			r.Post("/phantom/nonce", h.BeginPhantomAuth)
			r.Post("/phantom/verify", h.VerifyPhantomAuth)
			r.Post("/privy/verify", h.VerifyPrivyAuth)

			r.Group(func(r chi.Router) {
				r.Use(auth.Middleware(h.Auth, cfg))
				r.Get("/me", h.AuthMe)
				r.Post("/logout", h.Logout)
			})
		})

		// Groups
		r.Get("/groups", h.ListGroups)
		r.Get("/groups/{groupID}/swig/proposals", h.ListSwigProposals)
		r.Get("/groups/{groupID}/explorer", h.GetGroupExplorerLinks)
		r.Get("/groups/{groupID}/loans", h.ListLoanRequests)
		r.Get("/groups/{groupID}/yield", h.GetGroupYield)
		r.Get("/groups/{groupID}/cycle", h.GetGroupCycle)
		r.Get("/groups/{groupID}/members/{memberID}/credit-score", h.GetMemberCreditScore)
		r.Get("/agent/yield-rates", h.YieldRates)
		r.Get("/web3/balance", h.SolanaWalletBalance)
		r.Get("/onchain/config", h.OnchainConfig)
		r.Post("/webhooks/helius", h.HeliusWebhook)
		r.Get("/blinks/actions", h.ListBlinkActions)
		r.Get("/chain-events", h.ListChainEvents)

		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(h.Auth, cfg))
			r.Post("/groups", h.CreateGroup)
			r.Post("/groups/{groupID}/invite-links", h.CreateGroupInviteLink)
			r.Post("/groups/{groupID}/join", h.JoinGroup)
			r.Post("/groups/{groupID}/contribute", h.Contribute)
			r.Post("/groups/{groupID}/swig", h.ConfigureSwig)
			r.Post("/groups/{groupID}/swig/proposals", h.CreateSwigProposal)
			r.Post("/groups/{groupID}/swig/proposals/{proposalID}/approve", h.ApproveSwigProposal)
			r.Post("/groups/{groupID}/token", h.ConfigureToken2022)
			r.Post("/groups/{groupID}/token/validate-transfer", h.ValidateTokenTransfer)
			r.Post("/groups/{groupID}/loans", h.CreateLoanRequest)
			r.Post("/groups/{groupID}/loans/{loanID}/vote", h.VoteLoanRequest)
			r.Post("/groups/{groupID}/yield", h.CreateYieldEvent)
			r.Post("/groups/{groupID}/cycle/end", h.EndGroupCycle)
			r.Post("/agent/run", h.RunTreasuryAgent)
			r.Post("/blinks/{blinkType}/create", h.CreateBlink)
			r.Post("/blinks/{blinkType}/{actionID}/execute", h.ExecuteBlink)
			r.Post("/tx/build/{action}", h.BuildTx)
		})
	})

	if cfg.AgentAutoRun {
		treasury.StartScheduler(context.Background(), time.Duration(cfg.AgentIntervalSec)*time.Second, h)
	}

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("🚀 Ruby backend running on %s [env=%s]", addr, cfg.AppEnv)

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
