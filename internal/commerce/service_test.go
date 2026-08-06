package commerce

import (
	"context"
	"strings"
	"testing"
	"time"

	platformauth "seckill-mall/internal/platform/auth"
)

func newTestService(t *testing.T) (*Service, *MemoryRepository) {
	t.Helper()
	repository := NewMemoryRepository()
	authManager, err := platformauth.NewManager(strings.Repeat("j", 32), 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	service, err := NewService(repository, authManager, strings.Repeat("p", 32), 15*time.Minute)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	fixedNow := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }
	sequence := 0
	service.newID = func(prefix string) (string, error) {
		sequence++
		return prefix + "_test_" + string(rune('a'+sequence)), nil
	}
	return service, repository
}

func registerCustomerWithAddress(t *testing.T, service *Service, email string) (TokenPair, Address) {
	t.Helper()
	ctx := context.Background()
	tokens, err := service.Register(ctx, email, "password-123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	address, err := service.CreateAddress(ctx, tokens.User.ID, Address{
		Recipient: "测试用户",
		Phone:     "13800000000",
		Province:  "浙江省",
		City:      "杭州市",
		District:  "西湖区",
		Detail:    "测试路 1 号",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("CreateAddress() error = %v", err)
	}
	return tokens, address
}

func TestCompleteCommerceFlow(t *testing.T) {
	service, repository := newTestService(t)
	ctx := context.Background()
	if err := service.EnsureAdmin(ctx, "admin@example.com", "admin-password"); err != nil {
		t.Fatalf("EnsureAdmin() error = %v", err)
	}
	adminID := repository.FindUserIDByEmail("admin@example.com")
	user, address := registerCustomerWithAddress(t, service, "buyer@example.com")

	item, err := service.SetCartItem(ctx, user.User.ID, 1, 2)
	if err != nil || item.Subtotal != 1399800 {
		t.Fatalf("SetCartItem() = %+v, %v", item, err)
	}
	order, reused, err := service.CreateOrder(ctx, user.User.ID, address.ID, "checkout-1")
	if err != nil || reused || order.Status != OrderStatusPendingPayment {
		t.Fatalf("CreateOrder() = %+v, reused=%v, err=%v", order, reused, err)
	}
	duplicate, reused, err := service.CreateOrder(ctx, user.User.ID, address.ID, "checkout-1")
	if err != nil || !reused || duplicate.OrderID != order.OrderID {
		t.Fatalf("idempotent CreateOrder() = %+v, reused=%v, err=%v", duplicate, reused, err)
	}

	payment, signature, err := service.CreateMockPayment(ctx, user.User.ID, order.OrderID)
	if err != nil {
		t.Fatalf("CreateMockPayment() error = %v", err)
	}
	paid, err := service.CompleteMockPayment(ctx, payment.PaymentNo, "callback-1", signature)
	if err != nil || paid.Status != OrderStatusPaid {
		t.Fatalf("CompleteMockPayment() = %+v, %v", paid, err)
	}
	paidAgain, err := service.CompleteMockPayment(ctx, payment.PaymentNo, "callback-1", signature)
	if err != nil || paidAgain.Status != OrderStatusPaid {
		t.Fatalf("idempotent CompleteMockPayment() = %+v, %v", paidAgain, err)
	}

	shipped, err := service.ShipOrder(ctx, adminID, order.OrderID, "SF", "SF-001")
	if err != nil || shipped.Status != OrderStatusShipped || shipped.Shipment == nil {
		t.Fatalf("ShipOrder() = %+v, %v", shipped, err)
	}
	completed, err := service.ConfirmOrder(ctx, user.User.ID, order.OrderID)
	if err != nil || completed.Status != OrderStatusCompleted {
		t.Fatalf("ConfirmOrder() = %+v, %v", completed, err)
	}
	completedAgain, reused, err := service.CreateOrder(ctx, user.User.ID, address.ID, "checkout-1")
	if err != nil || !reused || completedAgain.Payment == nil || completedAgain.Shipment == nil {
		t.Fatalf("completed idempotent CreateOrder() = %+v, reused=%v, err=%v", completedAgain, reused, err)
	}
	if _, _, err := service.RefundOrder(ctx, user.User.ID, order.OrderID, "已完成订单退款"); ErrorCode(err) != CodeInvalidTransition {
		t.Fatalf("RefundOrder() error code = %s, err=%v", ErrorCode(err), err)
	}
}

func TestAdminProductUpdateCannotOverwriteReservedStock(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	user, address := registerCustomerWithAddress(t, service, "inventory@example.com")
	if _, err := service.SetCartItem(ctx, user.User.ID, 1, 1); err != nil {
		t.Fatalf("SetCartItem() error = %v", err)
	}
	if _, _, err := service.CreateOrder(ctx, user.User.ID, address.ID, "reserve-stock"); err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}

	product, err := service.UpsertProduct(ctx, AdminProductInput{
		Category: Category{ID: 1, Name: "数码产品", Slug: "digital"},
		SPU:      SPU{ID: 1, Name: "iPhone 15"},
		SKUs: []SKU{{
			ID: 1, Code: "IPHONE15-128-BLACK", Name: "iPhone 15 128GB 黑色",
			PriceCents: 699900, AvailableStock: 99, ReservedStock: 50,
		}},
	})
	if err != nil {
		t.Fatalf("UpsertProduct() error = %v", err)
	}
	if got := product.SKUs[0].ReservedStock; got != 1 {
		t.Fatalf("reserved stock = %d, want 1", got)
	}
}

func TestCancelRefundAndRefreshFlow(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	tokens, address := registerCustomerWithAddress(t, service, "buyer@example.com")

	rotated, err := service.Refresh(ctx, tokens.RefreshToken)
	if err != nil || rotated.RefreshToken == tokens.RefreshToken {
		t.Fatalf("Refresh() = %+v, %v", rotated, err)
	}
	if _, err := service.Refresh(ctx, tokens.RefreshToken); ErrorCode(err) != CodeUnauthorized {
		t.Fatalf("reused Refresh() error code = %s, err=%v", ErrorCode(err), err)
	}

	if _, err := service.SetCartItem(ctx, tokens.User.ID, 1, 1); err != nil {
		t.Fatalf("SetCartItem() error = %v", err)
	}
	canceledOrder, _, err := service.CreateOrder(ctx, tokens.User.ID, address.ID, "cancel-1")
	if err != nil {
		t.Fatalf("CreateOrder(cancel) error = %v", err)
	}
	canceledOrder, err = service.CancelOrder(ctx, tokens.User.ID, canceledOrder.OrderID, "不想要了")
	if err != nil || canceledOrder.Status != OrderStatusCanceled {
		t.Fatalf("CancelOrder() = %+v, %v", canceledOrder, err)
	}

	if _, err := service.SetCartItem(ctx, tokens.User.ID, 1, 1); err != nil {
		t.Fatalf("SetCartItem(refund) error = %v", err)
	}
	order, _, err := service.CreateOrder(ctx, tokens.User.ID, address.ID, "refund-1")
	if err != nil {
		t.Fatalf("CreateOrder(refund) error = %v", err)
	}
	payment, signature, err := service.CreateMockPayment(ctx, tokens.User.ID, order.OrderID)
	if err != nil {
		t.Fatalf("CreateMockPayment(refund) error = %v", err)
	}
	if _, err := service.CompleteMockPayment(ctx, payment.PaymentNo, "callback-refund", signature); err != nil {
		t.Fatalf("CompleteMockPayment(refund) error = %v", err)
	}
	refund, refundedOrder, err := service.RefundOrder(ctx, tokens.User.ID, order.OrderID, "改变主意")
	if err != nil || refund.Status != RefundStatusSucceeded || refundedOrder.Status != OrderStatusRefunded {
		t.Fatalf("RefundOrder() = %+v, %+v, %v", refund, refundedOrder, err)
	}

	product, err := service.GetProduct(ctx, 1)
	if err != nil || product.SKUs[0].AvailableStock != 100 || product.SKUs[0].ReservedStock != 0 {
		t.Fatalf("stock after cancel/refund = %+v, %v", product.SKUs[0], err)
	}
}

func TestAddressOwnership(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	_, firstAddress := registerCustomerWithAddress(t, service, "first@example.com")
	second, _ := registerCustomerWithAddress(t, service, "second@example.com")

	_, err := service.UpdateAddress(ctx, second.User.ID, firstAddress.ID, firstAddress)
	if ErrorCode(err) != CodeNotFound {
		t.Fatalf("UpdateAddress() error code = %s, err=%v", ErrorCode(err), err)
	}
	if err := service.DeleteAddress(ctx, second.User.ID, firstAddress.ID); ErrorCode(err) != CodeNotFound {
		t.Fatalf("DeleteAddress() error code = %s, err=%v", ErrorCode(err), err)
	}
}

func TestSeckillOrderUsesUnifiedStateMachine(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	user, address := registerCustomerWithAddress(t, service, "buyer@example.com")

	order, reused, err := service.CreateSeckillOrder(ctx, user.User.ID, address.ID, 1, 1, "seckill-1")
	if err != nil || reused || order.OrderType != OrderTypeSeckill || order.Status != OrderStatusPendingPayment {
		t.Fatalf("CreateSeckillOrder() = %+v, reused=%v, err=%v", order, reused, err)
	}
	payment, signature, err := service.CreateMockPayment(ctx, user.User.ID, order.OrderID)
	if err != nil {
		t.Fatalf("CreateMockPayment() error = %v", err)
	}
	paid, err := service.CompleteMockPayment(ctx, payment.PaymentNo, "seckill-callback", signature)
	if err != nil || paid.Status != OrderStatusPaid {
		t.Fatalf("CompleteMockPayment() = %+v, %v", paid, err)
	}
}

func TestOrderRejectsInsufficientStock(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	user, address := registerCustomerWithAddress(t, service, "stock@example.com")

	if _, _, err := service.CreateSeckillOrder(ctx, user.User.ID, address.ID, 1, 99, "stock-99"); err != nil {
		t.Fatalf("CreateSeckillOrder(99) error = %v", err)
	}
	if _, _, err := service.CreateSeckillOrder(ctx, user.User.ID, address.ID, 1, 2, "stock-overflow"); ErrorCode(err) != CodeOutOfStock {
		t.Fatalf("insufficient stock error code = %s, err=%v", ErrorCode(err), err)
	}
}

func TestExpireOrdersReleasesReservedStock(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	user, address := registerCustomerWithAddress(t, service, "expiry@example.com")
	if _, err := service.SetCartItem(ctx, user.User.ID, 1, 2); err != nil {
		t.Fatalf("SetCartItem() error = %v", err)
	}
	order, _, err := service.CreateOrder(ctx, user.User.ID, address.ID, "expiring-order")
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}

	service.now = func() time.Time { return order.ExpiresAt.Add(time.Second) }
	count, err := service.ExpireOrders(ctx, 10)
	if err != nil || count != 1 {
		t.Fatalf("ExpireOrders() = %d, %v", count, err)
	}
	expired, err := service.GetOrder(ctx, user.User.ID, order.OrderID)
	if err != nil || expired.Status != OrderStatusCanceled {
		t.Fatalf("expired order = %+v, %v", expired, err)
	}
	product, err := service.GetProduct(ctx, 1)
	if err != nil || product.SKUs[0].AvailableStock != 100 || product.SKUs[0].ReservedStock != 0 {
		t.Fatalf("stock after expiry = %+v, %v", product.SKUs[0], err)
	}
}
