package auth

import (
	"strings"
	"testing"

	"github.com/dev3pack/ruby/backend/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

func TestBeginPhantomLoginReturnsNonceAndMessage(t *testing.T) {
	svc := NewService(&config.Config{AuthDomain: "localhost:3000"}, nil)

	nonce, message, err := svc.BeginPhantomLogin("Wallet111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if nonce == "" {
		t.Fatal("expected nonce")
	}
	if !strings.Contains(message, nonce) {
		t.Fatalf("message should include nonce, got: %s", message)
	}
}

func TestContainsAudience(t *testing.T) {
	if !containsAudience("app-id", "app-id") {
		t.Fatal("expected string audience to match")
	}
	if !containsAudience([]any{"x", "y", "app-id"}, "app-id") {
		t.Fatal("expected []any audience to match")
	}
	if containsAudience([]string{"x", "y"}, "app-id") {
		t.Fatal("expected mismatch for []string")
	}
	if !containsAudience(jwt.ClaimStrings{"x", "app-id"}, "app-id") {
		t.Fatal("expected jwt.ClaimStrings audience to match")
	}
}

func TestAudienceContainsPrivyApp(t *testing.T) {
	if !audienceContainsPrivyApp(jwt.ClaimStrings{"cmabc", "cmxyz"}, "cmxyz") {
		t.Fatal("expected ClaimStrings to match app id")
	}
	if audienceContainsPrivyApp(jwt.ClaimStrings{"other"}, "cmxyz") {
		t.Fatal("expected mismatch")
	}
}

func TestValidatePhantomMessage(t *testing.T) {
	msg := "Sign in to Ruby.\nDomain: localhost:3000\nWallet: Wallet111\nNonce: nonce123"
	if err := validatePhantomMessage(msg, "localhost:3000", "Wallet111", "nonce123"); err != nil {
		t.Fatalf("expected valid message, got %v", err)
	}
}

func TestValidatePhantomMessageRejectsMismatchedDomain(t *testing.T) {
	msg := "Sign in to Ruby.\nDomain: evil.example\nWallet: Wallet111\nNonce: nonce123"
	if err := validatePhantomMessage(msg, "localhost:3000", "Wallet111", "nonce123"); err == nil {
		t.Fatal("expected domain mismatch error")
	}
}
