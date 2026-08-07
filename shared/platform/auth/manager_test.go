package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if err := VerifyPassword(hash, "correct-horse"); err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if err := VerifyPassword(hash, "wrong-password"); err == nil {
		t.Fatal("VerifyPassword() expected mismatch")
	}
}

func TestAccessTokenRoundTrip(t *testing.T) {
	manager, err := NewManager(strings.Repeat("s", 32), time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	token, _, err := manager.IssueAccessToken(42, "customer")
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v", err)
	}
	claims, err := manager.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("ParseAccessToken() error = %v", err)
	}
	if claims.UserID != 42 || claims.Role != "customer" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestRefreshTokenStoresOnlyHash(t *testing.T) {
	manager, err := NewManager(strings.Repeat("s", 32), time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	token, err := manager.IssueRefreshToken()
	if err != nil {
		t.Fatalf("IssueRefreshToken() error = %v", err)
	}
	if token.Raw == token.Hash || HashOpaqueToken(token.Raw) != token.Hash {
		t.Fatalf("refresh token hash mismatch: %+v", token)
	}
}
