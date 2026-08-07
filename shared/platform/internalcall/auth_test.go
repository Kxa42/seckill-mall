package internalcall

import (
	"context"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

func TestUserAndRoleAuthorization(t *testing.T) {
	secret := "internal-secret-value"
	now := time.Now().UTC()
	userCtx := incoming(AppendUser(context.Background(), secret, "/service/user", 9, now))
	if err := AuthorizeUser(userCtx, secret, "/service/user", 9, now); err != nil {
		t.Fatalf("AuthorizeUser() error = %v", err)
	}
	if err := AuthorizeUser(userCtx, secret, "/service/other", 9, now); err == nil {
		t.Fatal("AuthorizeUser() accepted another method")
	}
	roleCtx := incoming(AppendRole(context.Background(), secret, "/service/admin", 9, "admin", now))
	if err := AuthorizeRole(roleCtx, secret, "/service/admin", 9, "admin", now); err != nil {
		t.Fatalf("AuthorizeRole() error = %v", err)
	}
	if err := AuthorizeRole(roleCtx, secret, "/service/admin", 9, "customer", now); err == nil {
		t.Fatal("AuthorizeRole() accepted another role")
	}
}

func TestAuthorizationRejectsExpiredAndTamperedMetadata(t *testing.T) {
	secret := "internal-secret-value"
	now := time.Now().UTC()
	expired := incoming(AppendUser(context.Background(), secret, "/service/user", 9, now.Add(-time.Minute)))
	if err := AuthorizeUser(expired, secret, "/service/user", 9, now); err == nil {
		t.Fatal("AuthorizeUser() accepted expired metadata")
	}
	tampered := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		UserKey, "9", TimestampKey, strconv.FormatInt(now.Unix(), 10), SignatureKey, "tampered",
	))
	if err := AuthorizeUser(tampered, secret, "/service/user", 9, now); err == nil {
		t.Fatal("AuthorizeUser() accepted tampered metadata")
	}
}

func TestValidateSecret(t *testing.T) {
	if err := ValidateSecret("short"); err == nil {
		t.Fatal("ValidateSecret() accepted a short secret")
	}
	if err := ValidateSecret("12345678901234567890123456789012"); err != nil {
		t.Fatalf("ValidateSecret() error = %v", err)
	}
}

func incoming(ctx context.Context) context.Context {
	values, _ := metadata.FromOutgoingContext(ctx)
	return metadata.NewIncomingContext(context.Background(), values)
}
