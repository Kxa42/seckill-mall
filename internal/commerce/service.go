package commerce

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	platformauth "seckill-mall/internal/platform/auth"
)

// Service 编排商城后端用例，不依赖 HTTP 或具体数据库实现。
type Service struct {
	repository    Repository
	auth          *platformauth.Manager
	paymentSecret []byte
	orderTTL      time.Duration
	orderMode     string
	orderClient   OrderLifecycleClient
	now           func() time.Time
	newID         func(prefix string) (string, error)
}

// NewService 创建商城应用服务。
func NewService(repository Repository, authManager *platformauth.Manager, paymentSecret string, orderTTL time.Duration) (*Service, error) {
	if repository == nil || authManager == nil {
		return nil, errors.New("repository 和 auth manager 不能为空")
	}
	if len(paymentSecret) < 32 {
		return nil, errors.New("Mock 支付密钥长度不能小于 32")
	}
	if orderTTL <= 0 {
		return nil, errors.New("订单有效期必须大于 0")
	}
	return &Service{
		repository:    repository,
		auth:          authManager,
		paymentSecret: []byte(paymentSecret),
		orderTTL:      orderTTL,
		orderMode:     OrderWriteModeLegacy,
		now:           time.Now,
		newID:         randomID,
	}, nil
}

// ConfigureOrderMigration 设置订单写入模式和生命周期客户端。
// order_service 模式下 Commerce 只保留支付、退款和物流记录，不再直接改变订单状态。
func (s *Service) ConfigureOrderMigration(mode string, client OrderLifecycleClient) error {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = OrderWriteModeLegacy
	}
	if mode != OrderWriteModeLegacy && mode != OrderWriteModeOrderService {
		return fmt.Errorf("订单写入模式无效: %s", mode)
	}
	if mode == OrderWriteModeOrderService && client == nil {
		return errors.New("order_service 模式需要 Order Service 客户端")
	}
	s.orderMode = mode
	s.orderClient = client
	return nil
}

func (s *Service) LegacyOrderWritesEnabled() bool {
	return s.orderMode != OrderWriteModeOrderService
}

func (s *Service) Register(ctx context.Context, email, password string) (TokenPair, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return TokenPair{}, err
	}
	hash, err := platformauth.HashPassword(password)
	if err != nil {
		return TokenPair{}, NewError(CodeValidation, err.Error(), nil)
	}
	user, err := s.repository.CreateUser(ctx, normalized, hash, RoleCustomer, s.now().UTC())
	if err != nil {
		return TokenPair{}, err
	}
	return s.issueTokenPair(ctx, user)
}

func (s *Service) Login(ctx context.Context, email, password string) (TokenPair, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return TokenPair{}, NewError(CodeUnauthorized, "邮箱或密码错误", nil)
	}
	user, err := s.repository.FindUserByEmail(ctx, normalized)
	if err != nil || user.Status != UserStatusActive || platformauth.VerifyPassword(user.PasswordHash, password) != nil {
		return TokenPair{}, NewError(CodeUnauthorized, "邮箱或密码错误", nil)
	}
	return s.issueTokenPair(ctx, user)
}

func (s *Service) Refresh(ctx context.Context, rawToken string) (TokenPair, error) {
	if strings.TrimSpace(rawToken) == "" {
		return TokenPair{}, NewError(CodeUnauthorized, "刷新令牌无效", nil)
	}
	replacement, err := s.auth.IssueRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	record := RefreshTokenRecord{
		TokenHash: replacement.Hash,
		ExpiresAt: replacement.ExpiresAt,
		CreatedAt: s.now().UTC(),
	}
	user, err := s.repository.RotateRefreshToken(ctx, platformauth.HashOpaqueToken(rawToken), record, s.now().UTC())
	if err != nil {
		return TokenPair{}, err
	}
	access, accessExpires, err := s.auth.IssueAccessToken(user.ID, user.Role)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:      access,
		AccessExpiresAt:  accessExpires,
		RefreshToken:     replacement.Raw,
		RefreshExpiresAt: replacement.ExpiresAt,
		User:             user,
	}, nil
}

func (s *Service) EnsureAdmin(ctx context.Context, email, password string) error {
	if email == "" && password == "" {
		return nil
	}
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	hash, err := platformauth.HashPassword(password)
	if err != nil {
		return NewError(CodeValidation, err.Error(), nil)
	}
	return s.repository.EnsureAdmin(ctx, normalized, hash, s.now().UTC())
}

func (s *Service) CreateAddress(ctx context.Context, userID uint64, address Address) (Address, error) {
	address.UserID = userID
	if userID == 0 {
		return Address{}, NewError(CodeUnauthorized, "用户未认证", nil)
	}
	if err := validateAddress(address); err != nil {
		return Address{}, err
	}
	return s.repository.CreateAddress(ctx, address, s.now().UTC())
}

func (s *Service) UpdateAddress(ctx context.Context, userID, addressID uint64, address Address) (Address, error) {
	address.ID = addressID
	address.UserID = userID
	if userID == 0 || addressID == 0 {
		return Address{}, NewError(CodeValidation, "地址参数无效", nil)
	}
	if err := validateAddress(address); err != nil {
		return Address{}, err
	}
	return s.repository.UpdateAddress(ctx, address, s.now().UTC())
}

func (s *Service) DeleteAddress(ctx context.Context, userID, addressID uint64) error {
	if userID == 0 || addressID == 0 {
		return NewError(CodeValidation, "地址参数无效", nil)
	}
	return s.repository.DeleteAddress(ctx, userID, addressID)
}

func (s *Service) ListAddresses(ctx context.Context, userID uint64) ([]Address, error) {
	return s.repository.ListAddresses(ctx, userID)
}

func (s *Service) ListProducts(ctx context.Context, offset, limit int) (ProductPage, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repository.ListProducts(ctx, offset, limit)
}

func (s *Service) GetProduct(ctx context.Context, productID uint64) (Product, error) {
	if productID == 0 {
		return Product{}, NewError(CodeValidation, "商品 ID 无效", nil)
	}
	return s.repository.GetProduct(ctx, productID)
}

func (s *Service) UpsertProduct(ctx context.Context, input AdminProductInput) (Product, error) {
	if strings.TrimSpace(input.Category.Name) == "" || strings.TrimSpace(input.Category.Slug) == "" || strings.TrimSpace(input.SPU.Name) == "" || len(input.SKUs) == 0 {
		return Product{}, NewError(CodeValidation, "分类、商品和 SKU 不能为空", nil)
	}
	for _, sku := range input.SKUs {
		if strings.TrimSpace(sku.Code) == "" || strings.TrimSpace(sku.Name) == "" || sku.PriceCents < 0 || sku.AvailableStock < 0 {
			return Product{}, NewError(CodeValidation, "SKU 参数无效", nil)
		}
	}
	return s.repository.UpsertProduct(ctx, input, s.now().UTC())
}

func (s *Service) SetCartItem(ctx context.Context, userID, skuID uint64, quantity int32) (CartItem, error) {
	if userID == 0 || skuID == 0 {
		return CartItem{}, NewError(CodeValidation, "购物车参数无效", nil)
	}
	if _, err := CalculateSubtotal(0, quantity); err != nil {
		return CartItem{}, err
	}
	return s.repository.SetCartItem(ctx, userID, skuID, quantity, s.now().UTC())
}

func (s *Service) DeleteCartItem(ctx context.Context, userID, skuID uint64) error {
	if userID == 0 || skuID == 0 {
		return NewError(CodeValidation, "购物车参数无效", nil)
	}
	return s.repository.DeleteCartItem(ctx, userID, skuID)
}

func (s *Service) ListCartItems(ctx context.Context, userID uint64) ([]CartItem, error) {
	return s.repository.ListCartItems(ctx, userID)
}

func (s *Service) PreviewCheckout(ctx context.Context, userID uint64) (CheckoutPreview, error) {
	items, err := s.repository.ListCartItems(ctx, userID)
	if err != nil {
		return CheckoutPreview{}, err
	}
	if len(items) == 0 {
		return CheckoutPreview{}, NewError(CodeValidation, "购物车为空", nil)
	}
	preview := CheckoutPreview{Items: items, Available: true}
	for _, item := range items {
		if !item.SKU.Active || item.SKU.AvailableStock < item.Quantity {
			preview.Available = false
		}
		total, err := addAmount(preview.TotalAmountCents, item.Subtotal)
		if err != nil {
			return CheckoutPreview{}, err
		}
		preview.TotalAmountCents = total
	}
	return preview, nil
}

func (s *Service) CreateOrder(ctx context.Context, userID, addressID uint64, idempotencyKey string) (Order, bool, error) {
	return s.createOrder(ctx, CreateOrderCommand{
		UserID:         userID,
		AddressID:      addressID,
		OrderType:      OrderTypeNormal,
		IdempotencyKey: idempotencyKey,
	})
}

func (s *Service) CreateSeckillOrder(ctx context.Context, userID, addressID, skuID uint64, quantity int32, idempotencyKey string) (Order, bool, error) {
	if _, err := CalculateSubtotal(0, quantity); err != nil {
		return Order{}, false, err
	}
	return s.createOrder(ctx, CreateOrderCommand{
		UserID:         userID,
		AddressID:      addressID,
		OrderType:      OrderTypeSeckill,
		IdempotencyKey: idempotencyKey,
		Items:          []CreateOrderItem{{SKUID: skuID, Quantity: quantity}},
	})
}

func (s *Service) createOrder(ctx context.Context, command CreateOrderCommand) (Order, bool, error) {
	if !s.LegacyOrderWritesEnabled() {
		return Order{}, false, NewError(CodeConflict, "订单已切换至 Order Service", nil)
	}
	if command.UserID == 0 || command.AddressID == 0 {
		return Order{}, false, NewError(CodeValidation, "用户和地址不能为空", nil)
	}
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if command.IdempotencyKey == "" || len(command.IdempotencyKey) > 128 {
		return Order{}, false, NewError(CodeValidation, "Idempotency-Key 不能为空且长度不能超过 128", nil)
	}
	orderID, err := s.newID("ord")
	if err != nil {
		return Order{}, false, err
	}
	now := s.now().UTC()
	command.OrderID = orderID
	command.Now = now
	command.ExpiresAt = now.Add(s.orderTTL)
	return s.repository.CreateOrder(ctx, command)
}

func (s *Service) ListOrders(ctx context.Context, userID uint64, offset, limit int) ([]Order, int64, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repository.ListOrders(ctx, userID, offset, limit)
}

func (s *Service) GetOrder(ctx context.Context, userID uint64, orderID string) (Order, error) {
	return s.repository.GetOrder(ctx, userID, strings.TrimSpace(orderID))
}

func (s *Service) CancelOrder(ctx context.Context, userID uint64, orderID, reason string) (Order, error) {
	if !s.LegacyOrderWritesEnabled() {
		return Order{}, NewError(CodeConflict, "订单已切换至 Order Service", nil)
	}
	if strings.TrimSpace(reason) == "" {
		reason = "用户取消"
	}
	return s.repository.CancelOrder(ctx, userID, strings.TrimSpace(orderID), reason, s.now().UTC())
}

func (s *Service) ExpireOrders(ctx context.Context, limit int) (int, error) {
	if !s.LegacyOrderWritesEnabled() {
		return 0, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	return s.repository.ExpireOrders(ctx, s.now().UTC(), limit)
}

func (s *Service) CreateMockPayment(ctx context.Context, userID uint64, orderID string) (Payment, string, error) {
	paymentNo, err := s.newID("pay")
	if err != nil {
		return Payment{}, "", err
	}
	payment, err := s.repository.CreatePayment(ctx, userID, strings.TrimSpace(orderID), paymentNo, s.now().UTC())
	if err != nil {
		return Payment{}, "", err
	}
	return payment, s.signPayment(payment.PaymentNo), nil
}

func (s *Service) CompleteMockPayment(ctx context.Context, paymentNo, callbackRef, signature string) (Order, error) {
	paymentNo = strings.TrimSpace(paymentNo)
	callbackRef = strings.TrimSpace(callbackRef)
	if paymentNo == "" || callbackRef == "" || !hmac.Equal([]byte(signature), []byte(s.signPayment(paymentNo))) {
		return Order{}, NewError(CodeUnauthorized, "Mock 支付回调签名无效", nil)
	}
	if s.orderMode == OrderWriteModeOrderService {
		paymentRepository, ok := s.repository.(PaymentTransitionRepository)
		if !ok {
			return Order{}, NewError(CodeUnavailable, "支付过渡 Repository 不支持新订单链路", nil)
		}
		payment, err := paymentRepository.GetPayment(ctx, paymentNo)
		if err != nil {
			return Order{}, err
		}
		if _, err := s.orderClient.ConfirmPayment(ctx, payment.UserID, payment.OrderID, paymentNo, callbackRef); err != nil {
			return Order{}, err
		}
		if _, err := paymentRepository.MarkPaymentSucceeded(ctx, paymentNo, callbackRef, s.now().UTC()); err != nil {
			return Order{}, err
		}
		return s.repository.GetOrder(ctx, payment.UserID, payment.OrderID)
	}
	return s.repository.CompletePayment(ctx, paymentNo, callbackRef, s.now().UTC())
}

func (s *Service) ShipOrder(ctx context.Context, actorID uint64, orderID, carrier, trackingNo string) (Order, error) {
	carrier = strings.TrimSpace(carrier)
	trackingNo = strings.TrimSpace(trackingNo)
	if carrier == "" || trackingNo == "" || len(carrier) > 64 || len(trackingNo) > 128 {
		return Order{}, NewError(CodeValidation, "物流公司和运单号无效", nil)
	}
	if s.orderMode == OrderWriteModeOrderService {
		if _, err := s.orderClient.Ship(ctx, actorID, strings.TrimSpace(orderID), carrier, trackingNo); err != nil {
			return Order{}, err
		}
		if recorder, ok := s.repository.(FulfillmentTransitionRepository); ok {
			if _, err := recorder.RecordShipment(ctx, strings.TrimSpace(orderID), carrier, trackingNo, s.now().UTC()); err != nil {
				return Order{}, err
			}
		}
		return s.repository.GetOrder(ctx, 0, strings.TrimSpace(orderID))
	}
	return s.repository.ShipOrder(ctx, strings.TrimSpace(orderID), carrier, trackingNo, actorID, s.now().UTC())
}

func (s *Service) ConfirmOrder(ctx context.Context, userID uint64, orderID string) (Order, error) {
	if s.orderMode == OrderWriteModeOrderService {
		if _, err := s.orderClient.ConfirmReceipt(ctx, userID, strings.TrimSpace(orderID)); err != nil {
			return Order{}, err
		}
		if recorder, ok := s.repository.(FulfillmentTransitionRepository); ok {
			if _, err := recorder.MarkShipmentReceived(ctx, strings.TrimSpace(orderID), s.now().UTC()); err != nil {
				return Order{}, err
			}
		}
		return s.repository.GetOrder(ctx, userID, strings.TrimSpace(orderID))
	}
	return s.repository.ConfirmOrder(ctx, userID, strings.TrimSpace(orderID), s.now().UTC())
}

func (s *Service) RefundOrder(ctx context.Context, userID uint64, orderID, reason string) (Refund, Order, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 255 {
		return Refund{}, Order{}, NewError(CodeValidation, "退款原因不能为空且长度不能超过 255", nil)
	}
	refundNo, err := s.newID("ref")
	if err != nil {
		return Refund{}, Order{}, err
	}
	if s.orderMode == OrderWriteModeOrderService {
		if _, err := s.orderClient.Refund(ctx, userID, strings.TrimSpace(orderID), refundNo, reason); err != nil {
			return Refund{}, Order{}, err
		}
		var refund Refund
		if recorder, ok := s.repository.(FulfillmentTransitionRepository); ok {
			refund, err = recorder.RecordRefund(ctx, userID, strings.TrimSpace(orderID), refundNo, reason, s.now().UTC())
			if err != nil {
				// Order Service 已完成状态转换；重试本接口会幂等确认订单并补写过渡退款记录。
				return Refund{}, Order{}, err
			}
		}
		value, err := s.repository.GetOrder(ctx, userID, strings.TrimSpace(orderID))
		return refund, value, err
	}
	return s.repository.RefundOrder(ctx, userID, strings.TrimSpace(orderID), refundNo, reason, s.now().UTC())
}

func (s *Service) issueTokenPair(ctx context.Context, user User) (TokenPair, error) {
	access, accessExpires, err := s.auth.IssueAccessToken(user.ID, user.Role)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, err := s.auth.IssueRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.repository.StoreRefreshToken(ctx, RefreshTokenRecord{
		UserID:    user.ID,
		TokenHash: refresh.Hash,
		ExpiresAt: refresh.ExpiresAt,
		CreatedAt: s.now().UTC(),
	}); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:      access,
		AccessExpiresAt:  accessExpires,
		RefreshToken:     refresh.Raw,
		RefreshExpiresAt: refresh.ExpiresAt,
		User:             user,
	}, nil
}

func (s *Service) signPayment(paymentNo string) string {
	mac := hmac.New(sha256.New, s.paymentSecret)
	mac.Write([]byte(paymentNo))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomID(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("生成业务 ID: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(buffer), nil
}
