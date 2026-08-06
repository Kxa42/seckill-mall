package order

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

func TestSignedInternalMetadataRoundTrip(t *testing.T) {
	secret := "internal-secret"
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	ctx := AppendSignedMetadata(context.Background(), secret, "/order/test", 9, now)
	values, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("signed metadata missing")
	}
	if values.Get(internalSignatureKey)[0] == "" {
		t.Fatal("signature missing")
	}

	t.Setenv("SECKILL_INTERNAL_CALL_SECRET", secret)
	// AuthorizeInternalRequest uses wall-clock validation; use a fresh timestamp for that check.
	ctx = AppendSignedMetadata(context.Background(), secret, "/order/test", 9, time.Now())
	ctx = metadata.NewIncomingContext(context.Background(), outgoingToIncoming(ctx))
	if err := AuthorizeInternalRequest(ctx, "/order/test", 9); err != nil {
		t.Fatalf("AuthorizeInternalRequest() error = %v", err)
	}
}

func TestSignedRoleMetadataRejectsCustomerForAdminCall(t *testing.T) {
	secret := "internal-secret"
	t.Setenv("SECKILL_INTERNAL_CALL_SECRET", secret)
	ctx := AppendSignedRoleMetadata(context.Background(), secret, "/order/ship", 9, "customer", time.Now())
	ctx = metadata.NewIncomingContext(context.Background(), outgoingToIncoming(ctx))
	if err := AuthorizeInternalRoleRequest(ctx, "/order/ship", 9, "admin"); err == nil {
		t.Fatal("customer role must not authorize admin operation")
	}
	ctx = AppendSignedRoleMetadata(context.Background(), secret, "/order/ship", 9, "admin", time.Now())
	ctx = metadata.NewIncomingContext(context.Background(), outgoingToIncoming(ctx))
	if err := AuthorizeInternalRoleRequest(ctx, "/order/ship", 9, "admin"); err != nil {
		t.Fatalf("admin role should authorize admin operation: %v", err)
	}
}

func TestExpireRequiresSystemRoleWhenInternalAuthEnabled(t *testing.T) {
	secret := "internal-secret"
	t.Setenv("SECKILL_INTERNAL_CALL_SECRET", secret)
	method := "/order/expire"
	ctx := AppendSignedRoleMetadata(context.Background(), secret, method, systemActorID, "admin", time.Now())
	ctx = metadata.NewIncomingContext(context.Background(), outgoingToIncoming(ctx))
	if err := AuthorizeInternalRoleRequest(ctx, method, systemActorID, systemRole); err == nil {
		t.Fatal("admin role must not authorize system expiry")
	}
	ctx = AppendSignedRoleMetadata(context.Background(), secret, method, systemActorID, systemRole, time.Now())
	ctx = metadata.NewIncomingContext(context.Background(), outgoingToIncoming(ctx))
	if err := AuthorizeInternalRoleRequest(ctx, method, systemActorID, systemRole); err != nil {
		t.Fatalf("system role should authorize expiry: %v", err)
	}
}

func outgoingToIncoming(ctx context.Context) metadata.MD {
	values, _ := metadata.FromOutgoingContext(ctx)
	return values
}
