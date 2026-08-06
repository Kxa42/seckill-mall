package commerce

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryRepository 是并发安全的验收测试存储，实现与 MySQL Repository 相同的业务语义。
type MemoryRepository struct {
	mu sync.Mutex

	nextID uint64

	users            map[uint64]User
	userByEmail      map[string]uint64
	refreshTokens    map[string]RefreshTokenRecord
	addresses        map[uint64]Address
	categories       map[uint64]Category
	spus             map[uint64]SPU
	skus             map[uint64]SKU
	images           map[uint64][]string
	carts            map[uint64]map[uint64]CartItem
	orders           map[string]Order
	orderIdempotency map[string]string
	reservations     map[string][]InventoryReservation
	payments         map[string]Payment
	paymentByOrder   map[string]string
	callbackRefs     map[string]string
	refunds          map[string]Refund
	refundByOrder    map[string]string
	shipments        map[string]Shipment
}

// NewMemoryRepository 创建带一个演示 SKU 的内存存储。
func NewMemoryRepository() *MemoryRepository {
	now := time.Now().UTC()
	return &MemoryRepository{
		nextID:           10,
		users:            make(map[uint64]User),
		userByEmail:      make(map[string]uint64),
		refreshTokens:    make(map[string]RefreshTokenRecord),
		addresses:        make(map[uint64]Address),
		categories:       map[uint64]Category{1: {ID: 1, Name: "数码产品", Slug: "digital", Active: true}},
		spus:             map[uint64]SPU{1: {ID: 1, CategoryID: 1, Name: "iPhone 15", Description: "演示商品", Active: true}},
		skus:             map[uint64]SKU{1: {ID: 1, SPUID: 1, Code: "IPHONE15-128-BLACK", Name: "iPhone 15 128GB 黑色", PriceCents: 699900, AvailableStock: 100, Active: true, CreatedAt: now, UpdatedAt: now}},
		images:           make(map[uint64][]string),
		carts:            make(map[uint64]map[uint64]CartItem),
		orders:           make(map[string]Order),
		orderIdempotency: make(map[string]string),
		reservations:     make(map[string][]InventoryReservation),
		payments:         make(map[string]Payment),
		paymentByOrder:   make(map[string]string),
		callbackRefs:     make(map[string]string),
		refunds:          make(map[string]Refund),
		refundByOrder:    make(map[string]string),
		shipments:        make(map[string]Shipment),
	}
}

func (r *MemoryRepository) next() uint64 {
	r.nextID++
	return r.nextID
}

func (r *MemoryRepository) CreateUser(_ context.Context, email, passwordHash, role string, now time.Time) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.userByEmail[email]; exists {
		return User{}, NewError(CodeConflict, "邮箱已注册", nil)
	}
	user := User{ID: r.next(), Email: email, PasswordHash: passwordHash, Role: role, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	r.users[user.ID] = user
	r.userByEmail[email] = user.ID
	return user, nil
}

func (r *MemoryRepository) FindUserByEmail(_ context.Context, email string) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, exists := r.userByEmail[email]
	if !exists {
		return User{}, NewError(CodeNotFound, "用户不存在", nil)
	}
	return r.users[id], nil
}

func (r *MemoryRepository) FindUserByID(_ context.Context, userID uint64) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, exists := r.users[userID]
	if !exists {
		return User{}, NewError(CodeNotFound, "用户不存在", nil)
	}
	return user, nil
}

func (r *MemoryRepository) StoreRefreshToken(_ context.Context, token RefreshTokenRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.refreshTokens[token.TokenHash]; exists {
		return NewError(CodeConflict, "刷新令牌重复", nil)
	}
	token.ID = r.next()
	r.refreshTokens[token.TokenHash] = token
	return nil
}

func (r *MemoryRepository) RotateRefreshToken(_ context.Context, oldHash string, replacement RefreshTokenRecord, now time.Time) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.refreshTokens[oldHash]
	if !exists || current.RevokedAt != nil || !current.ExpiresAt.After(now) {
		return User{}, NewError(CodeUnauthorized, "刷新令牌无效或已过期", nil)
	}
	revokedAt := now
	current.RevokedAt = &revokedAt
	r.refreshTokens[oldHash] = current
	replacement.ID = r.next()
	replacement.UserID = current.UserID
	r.refreshTokens[replacement.TokenHash] = replacement
	user, exists := r.users[current.UserID]
	if !exists || user.Status != UserStatusActive {
		return User{}, NewError(CodeUnauthorized, "用户不可用", nil)
	}
	return user, nil
}

func (r *MemoryRepository) EnsureAdmin(_ context.Context, email, passwordHash string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, exists := r.userByEmail[email]; exists {
		user := r.users[id]
		user.Role = RoleAdmin
		user.PasswordHash = passwordHash
		user.Status = UserStatusActive
		user.UpdatedAt = now
		r.users[id] = user
		return nil
	}
	user := User{ID: r.next(), Email: email, PasswordHash: passwordHash, Role: RoleAdmin, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	r.users[user.ID] = user
	r.userByEmail[email] = user.ID
	return nil
}

func (r *MemoryRepository) CreateAddress(_ context.Context, address Address, now time.Time) (Address, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.users[address.UserID]; !exists {
		return Address{}, NewError(CodeNotFound, "用户不存在", nil)
	}
	if address.IsDefault {
		r.clearDefaultAddress(address.UserID, 0, now)
	}
	address.ID = r.next()
	address.CreatedAt = now
	address.UpdatedAt = now
	r.addresses[address.ID] = address
	return address, nil
}

func (r *MemoryRepository) UpdateAddress(_ context.Context, address Address, now time.Time) (Address, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.addresses[address.ID]
	if !exists || current.UserID != address.UserID {
		return Address{}, NewError(CodeNotFound, "地址不存在", nil)
	}
	if address.IsDefault {
		r.clearDefaultAddress(address.UserID, address.ID, now)
	}
	address.CreatedAt = current.CreatedAt
	address.UpdatedAt = now
	r.addresses[address.ID] = address
	return address, nil
}

func (r *MemoryRepository) clearDefaultAddress(userID, exceptID uint64, now time.Time) {
	for id, address := range r.addresses {
		if address.UserID == userID && id != exceptID && address.IsDefault {
			address.IsDefault = false
			address.UpdatedAt = now
			r.addresses[id] = address
		}
	}
}

func (r *MemoryRepository) DeleteAddress(_ context.Context, userID, addressID uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	address, exists := r.addresses[addressID]
	if !exists || address.UserID != userID {
		return NewError(CodeNotFound, "地址不存在", nil)
	}
	delete(r.addresses, addressID)
	return nil
}

func (r *MemoryRepository) ListAddresses(_ context.Context, userID uint64) ([]Address, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	addresses := make([]Address, 0)
	for _, address := range r.addresses {
		if address.UserID == userID {
			addresses = append(addresses, address)
		}
	}
	sort.Slice(addresses, func(i, j int) bool {
		if addresses[i].IsDefault != addresses[j].IsDefault {
			return addresses[i].IsDefault
		}
		return addresses[i].ID < addresses[j].ID
	})
	return addresses, nil
}

func (r *MemoryRepository) ListProducts(_ context.Context, offset, limit int) (ProductPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]uint64, 0)
	for id, spu := range r.spus {
		if spu.Active {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	total := int64(len(ids))
	if offset > len(ids) {
		offset = len(ids)
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	products := make([]Product, 0, end-offset)
	for _, id := range ids[offset:end] {
		products = append(products, r.productLocked(id, false))
	}
	return ProductPage{Items: products, Offset: offset, Limit: limit, Total: total}, nil
}

func (r *MemoryRepository) GetProduct(_ context.Context, productID uint64) (Product, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	spu, exists := r.spus[productID]
	if !exists || !spu.Active {
		return Product{}, NewError(CodeNotFound, "商品不存在或未上架", nil)
	}
	return r.productLocked(productID, false), nil
}

func (r *MemoryRepository) productLocked(productID uint64, includeInactive bool) Product {
	product := Product{SPU: r.spus[productID], Images: append([]string(nil), r.images[productID]...)}
	for _, sku := range r.skus {
		if sku.SPUID == productID && (includeInactive || sku.Active) {
			product.SKUs = append(product.SKUs, sku)
		}
	}
	sort.Slice(product.SKUs, func(i, j int) bool { return product.SKUs[i].ID < product.SKUs[j].ID })
	return product
}

func (r *MemoryRepository) UpsertProduct(_ context.Context, input AdminProductInput, now time.Time) (Product, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	category := input.Category
	if category.ID == 0 {
		category.ID = r.next()
	}
	category.Active = true
	r.categories[category.ID] = category

	spu := input.SPU
	if spu.ID == 0 {
		spu.ID = r.next()
	}
	spu.CategoryID = category.ID
	spu.Active = true
	r.spus[spu.ID] = spu
	for _, sku := range input.SKUs {
		if sku.ID == 0 {
			sku.ID = r.next()
			sku.ReservedStock = 0
		} else if existing, exists := r.skus[sku.ID]; exists {
			sku.ReservedStock = existing.ReservedStock
			sku.CreatedAt = existing.CreatedAt
		}
		sku.SPUID = spu.ID
		sku.Active = true
		if sku.CreatedAt.IsZero() {
			sku.CreatedAt = now
		}
		sku.UpdatedAt = now
		r.skus[sku.ID] = sku
	}
	return r.productLocked(spu.ID, true), nil
}

func (r *MemoryRepository) SetCartItem(_ context.Context, userID, skuID uint64, quantity int32, now time.Time) (CartItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sku, exists := r.skus[skuID]
	if !exists || !sku.Active {
		return CartItem{}, NewError(CodeNotFound, "SKU 不存在或未上架", nil)
	}
	if r.carts[userID] == nil {
		r.carts[userID] = make(map[uint64]CartItem)
	}
	item, exists := r.carts[userID][skuID]
	if !exists {
		item.ID = r.next()
		item.CreatedAt = now
	}
	item.UserID = userID
	item.SKUID = skuID
	item.Quantity = quantity
	item.SKU = sku
	item.Subtotal, _ = CalculateSubtotal(sku.PriceCents, quantity)
	item.UpdatedAt = now
	r.carts[userID][skuID] = item
	return item, nil
}

func (r *MemoryRepository) DeleteCartItem(_ context.Context, userID, skuID uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.carts[userID][skuID]; !exists {
		return NewError(CodeNotFound, "购物车商品不存在", nil)
	}
	delete(r.carts[userID], skuID)
	return nil
}

func (r *MemoryRepository) ListCartItems(_ context.Context, userID uint64) ([]CartItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]CartItem, 0, len(r.carts[userID]))
	for _, item := range r.carts[userID] {
		item.SKU = r.skus[item.SKUID]
		item.Subtotal, _ = CalculateSubtotal(item.SKU.PriceCents, item.Quantity)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *MemoryRepository) CreateOrder(_ context.Context, command CreateOrderCommand) (Order, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idempotencyIndex := fmt.Sprintf("%d:%s", command.UserID, command.IdempotencyKey)
	if orderID, exists := r.orderIdempotency[idempotencyIndex]; exists {
		return cloneOrder(r.attachOrderDetails(r.orders[orderID])), true, nil
	}
	address, exists := r.addresses[command.AddressID]
	if !exists || address.UserID != command.UserID {
		return Order{}, false, NewError(CodeNotFound, "收货地址不存在", nil)
	}

	requested := make(map[uint64]int32)
	if len(command.Items) == 0 {
		for skuID, item := range r.carts[command.UserID] {
			requested[skuID] += item.Quantity
		}
	} else {
		for _, item := range command.Items {
			requested[item.SKUID] += item.Quantity
		}
	}
	if len(requested) == 0 {
		return Order{}, false, NewError(CodeValidation, "没有可结算商品", nil)
	}

	orderItems := make([]OrderItem, 0, len(requested))
	var total int64
	for skuID, quantity := range requested {
		sku, exists := r.skus[skuID]
		if !exists || !sku.Active {
			return Order{}, false, NewError(CodeNotFound, "SKU 不存在或未上架", nil)
		}
		if quantity <= 0 || quantity > 99 {
			return Order{}, false, NewError(CodeValidation, "商品数量无效", nil)
		}
		if sku.AvailableStock < quantity {
			return Order{}, false, NewError(CodeOutOfStock, "库存不足", nil)
		}
		subtotal, err := CalculateSubtotal(sku.PriceCents, quantity)
		if err != nil {
			return Order{}, false, err
		}
		total, err = addAmount(total, subtotal)
		if err != nil {
			return Order{}, false, err
		}
		orderItems = append(orderItems, OrderItem{
			ID:             r.next(),
			OrderID:        command.OrderID,
			SKUID:          sku.ID,
			SKUCode:        sku.Code,
			SKUName:        sku.Name,
			UnitPriceCents: sku.PriceCents,
			Quantity:       quantity,
			SubtotalCents:  subtotal,
		})
	}
	sort.Slice(orderItems, func(i, j int) bool { return orderItems[i].SKUID < orderItems[j].SKUID })

	reservations := make([]InventoryReservation, 0, len(orderItems))
	for _, item := range orderItems {
		sku := r.skus[item.SKUID]
		sku.AvailableStock -= item.Quantity
		sku.ReservedStock += item.Quantity
		sku.UpdatedAt = command.Now
		r.skus[item.SKUID] = sku
		reservations = append(reservations, InventoryReservation{
			ID:            r.next(),
			ReservationID: fmt.Sprintf("res_%s_%d", command.OrderID, item.SKUID),
			OrderID:       command.OrderID,
			SKUID:         item.SKUID,
			Quantity:      item.Quantity,
			Status:        ReservationStatusReserved,
			ExpiresAt:     command.ExpiresAt,
			CreatedAt:     command.Now,
			UpdatedAt:     command.Now,
		})
	}

	order := Order{
		ID:               r.next(),
		OrderID:          command.OrderID,
		UserID:           command.UserID,
		OrderType:        command.OrderType,
		Status:           OrderStatusPendingPayment,
		TotalAmountCents: total,
		IdempotencyKey:   command.IdempotencyKey,
		AddressSnapshot:  address,
		Items:            orderItems,
		ExpiresAt:        command.ExpiresAt,
		CreatedAt:        command.Now,
		UpdatedAt:        command.Now,
		StatusHistory: []OrderStatus{{
			ID: r.next(), OrderID: command.OrderID, ToStatus: OrderStatusPendingPayment,
			Reason: "创建订单", ActorType: "user", ActorID: command.UserID, CreatedAt: command.Now,
		}},
	}
	r.orders[order.OrderID] = order
	r.orderIdempotency[idempotencyIndex] = order.OrderID
	r.reservations[order.OrderID] = reservations
	if command.OrderType == OrderTypeNormal {
		delete(r.carts, command.UserID)
	}
	return cloneOrder(order), false, nil
}

func (r *MemoryRepository) ListOrders(_ context.Context, userID uint64, offset, limit int) ([]Order, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	orders := make([]Order, 0)
	for _, order := range r.orders {
		if order.UserID == userID {
			orders = append(orders, cloneOrder(r.attachOrderDetails(order)))
		}
	}
	sort.Slice(orders, func(i, j int) bool { return orders[i].CreatedAt.After(orders[j].CreatedAt) })
	total := int64(len(orders))
	if offset > len(orders) {
		offset = len(orders)
	}
	end := offset + limit
	if end > len(orders) {
		end = len(orders)
	}
	return orders[offset:end], total, nil
}

func (r *MemoryRepository) GetOrder(_ context.Context, userID uint64, orderID string) (Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, exists := r.orders[orderID]
	if !exists || order.UserID != userID {
		return Order{}, NewError(CodeNotFound, "订单不存在", nil)
	}
	return cloneOrder(r.attachOrderDetails(order)), nil
}

func (r *MemoryRepository) CancelOrder(_ context.Context, userID uint64, orderID, reason string, now time.Time) (Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, exists := r.orders[orderID]
	if !exists || order.UserID != userID {
		return Order{}, NewError(CodeNotFound, "订单不存在", nil)
	}
	if order.Status == OrderStatusCanceled {
		return cloneOrder(r.attachOrderDetails(order)), nil
	}
	if !CanTransitionOrder(order.Status, OrderStatusCanceled) {
		return Order{}, NewError(CodeInvalidTransition, "当前订单状态不能取消", nil)
	}
	r.releaseReservations(orderID, ReservationStatusReleased, now)
	r.transitionOrder(&order, OrderStatusCanceled, reason, "user", userID, now)
	order.CanceledAt = timePointer(now)
	r.orders[orderID] = order
	return cloneOrder(r.attachOrderDetails(order)), nil
}

func (r *MemoryRepository) ExpireOrders(_ context.Context, now time.Time, limit int) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for orderID, order := range r.orders {
		if count >= limit {
			break
		}
		if order.Status != OrderStatusPendingPayment || order.ExpiresAt.After(now) {
			continue
		}
		r.releaseReservations(orderID, ReservationStatusExpired, now)
		r.transitionOrder(&order, OrderStatusCanceled, "支付超时", "system", 0, now)
		order.CanceledAt = timePointer(now)
		r.orders[orderID] = order
		count++
	}
	return count, nil
}

func (r *MemoryRepository) CreatePayment(_ context.Context, userID uint64, orderID, paymentNo string, now time.Time) (Payment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, exists := r.orders[orderID]
	if !exists || order.UserID != userID {
		return Payment{}, NewError(CodeNotFound, "订单不存在", nil)
	}
	if order.Status != OrderStatusPendingPayment || !order.ExpiresAt.After(now) {
		return Payment{}, NewError(CodeInvalidTransition, "当前订单不能支付", nil)
	}
	if existingNo, exists := r.paymentByOrder[orderID]; exists {
		return r.payments[existingNo], nil
	}
	payment := Payment{
		ID: r.next(), PaymentNo: paymentNo, OrderID: orderID, AmountCents: order.TotalAmountCents,
		Provider: "mock", Status: PaymentStatusPending, CreatedAt: now, UpdatedAt: now,
	}
	r.payments[paymentNo] = payment
	r.paymentByOrder[orderID] = paymentNo
	return payment, nil
}

func (r *MemoryRepository) GetPayment(_ context.Context, paymentNo string) (Payment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	payment, exists := r.payments[paymentNo]
	if !exists {
		return Payment{}, NewError(CodeNotFound, "支付单不存在", nil)
	}
	if order, orderExists := r.orders[payment.OrderID]; orderExists {
		payment.UserID = order.UserID
	}
	return payment, nil
}

func (r *MemoryRepository) MarkPaymentSucceeded(_ context.Context, paymentNo, callbackRef string, now time.Time) (Payment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	payment, exists := r.payments[paymentNo]
	if !exists {
		return Payment{}, NewError(CodeNotFound, "支付单不存在", nil)
	}
	if owner, exists := r.callbackRefs[callbackRef]; exists && owner != paymentNo {
		return Payment{}, NewError(CodeConflict, "支付回调流水已使用", nil)
	}
	if payment.Status == PaymentStatusSucceeded {
		if payment.CallbackRef != callbackRef {
			return Payment{}, NewError(CodeConflict, "支付回调与已完成记录不一致", nil)
		}
		return payment, nil
	}
	payment.Status = PaymentStatusSucceeded
	payment.CallbackRef = callbackRef
	payment.PaidAt = timePointer(now)
	payment.UpdatedAt = now
	r.payments[paymentNo] = payment
	r.callbackRefs[callbackRef] = paymentNo
	return payment, nil
}

func (r *MemoryRepository) RecordShipment(_ context.Context, orderID, carrier, trackingNo string, now time.Time) (Shipment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.orders[orderID]; !exists {
		return Shipment{}, NewError(CodeNotFound, "订单不存在", nil)
	}
	if existing, exists := r.shipments[orderID]; exists {
		if existing.Carrier != carrier || existing.TrackingNo != trackingNo {
			return Shipment{}, NewError(CodeConflict, "订单已有不同物流记录", nil)
		}
		return existing, nil
	}
	shipment := Shipment{ID: r.next(), OrderID: orderID, Carrier: carrier, TrackingNo: trackingNo, Status: ShipmentStatusShipped, ShippedAt: now, CreatedAt: now, UpdatedAt: now}
	r.shipments[orderID] = shipment
	return shipment, nil
}

func (r *MemoryRepository) MarkShipmentReceived(_ context.Context, orderID string, now time.Time) (Shipment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	shipment, exists := r.shipments[orderID]
	if !exists {
		return Shipment{}, NewError(CodeNotFound, "物流记录不存在", nil)
	}
	shipment.Status = ShipmentStatusReceived
	shipment.DeliveredAt = timePointer(now)
	shipment.UpdatedAt = now
	r.shipments[orderID] = shipment
	return shipment, nil
}

func (r *MemoryRepository) RecordRefund(_ context.Context, userID uint64, orderID, refundNo, reason string, now time.Time) (Refund, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, exists := r.orders[orderID]
	if !exists || order.UserID != userID {
		return Refund{}, NewError(CodeNotFound, "订单不存在", nil)
	}
	if existingNo, exists := r.refundByOrder[orderID]; exists {
		return r.refunds[existingNo], nil
	}
	paymentNo, exists := r.paymentByOrder[orderID]
	if !exists || r.payments[paymentNo].Status != PaymentStatusSucceeded {
		return Refund{}, NewError(CodeInvalidTransition, "订单没有成功支付记录", nil)
	}
	refund := Refund{ID: r.next(), RefundNo: refundNo, OrderID: orderID, PaymentNo: paymentNo, AmountCents: order.TotalAmountCents, Reason: reason, Status: RefundStatusSucceeded, CreatedAt: now, CompletedAt: timePointer(now)}
	r.refunds[refundNo] = refund
	r.refundByOrder[orderID] = refundNo
	return refund, nil
}

func (r *MemoryRepository) CompletePayment(_ context.Context, paymentNo, callbackRef string, now time.Time) (Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	payment, exists := r.payments[paymentNo]
	if !exists {
		return Order{}, NewError(CodeNotFound, "支付单不存在", nil)
	}
	if owner, exists := r.callbackRefs[callbackRef]; exists && owner != paymentNo {
		return Order{}, NewError(CodeConflict, "支付回调流水已使用", nil)
	}
	order := r.orders[payment.OrderID]
	if payment.Status == PaymentStatusSucceeded {
		if payment.CallbackRef != callbackRef {
			return Order{}, NewError(CodeConflict, "支付回调与已完成记录不一致", nil)
		}
		return cloneOrder(r.attachOrderDetails(order)), nil
	}
	if !CanTransitionOrder(order.Status, OrderStatusPaid) {
		return Order{}, NewError(CodeInvalidTransition, "当前订单不能完成支付", nil)
	}
	payment.Status = PaymentStatusSucceeded
	payment.CallbackRef = callbackRef
	payment.PaidAt = timePointer(now)
	payment.UpdatedAt = now
	r.payments[paymentNo] = payment
	r.callbackRefs[callbackRef] = paymentNo
	for index, reservation := range r.reservations[order.OrderID] {
		if reservation.Status != ReservationStatusReserved {
			continue
		}
		sku := r.skus[reservation.SKUID]
		sku.ReservedStock -= reservation.Quantity
		sku.UpdatedAt = now
		r.skus[reservation.SKUID] = sku
		reservation.Status = ReservationStatusConfirmed
		reservation.UpdatedAt = now
		r.reservations[order.OrderID][index] = reservation
	}
	r.transitionOrder(&order, OrderStatusPaid, "Mock 支付成功", "payment", 0, now)
	order.PaidAt = timePointer(now)
	r.orders[order.OrderID] = order
	return cloneOrder(r.attachOrderDetails(order)), nil
}

func (r *MemoryRepository) ShipOrder(_ context.Context, orderID, carrier, trackingNo string, actorID uint64, now time.Time) (Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, exists := r.orders[orderID]
	if !exists {
		return Order{}, NewError(CodeNotFound, "订单不存在", nil)
	}
	if order.Status == OrderStatusShipped {
		return cloneOrder(r.attachOrderDetails(order)), nil
	}
	if !CanTransitionOrder(order.Status, OrderStatusShipped) {
		return Order{}, NewError(CodeInvalidTransition, "当前订单不能发货", nil)
	}
	shipment := Shipment{
		ID: r.next(), OrderID: orderID, Carrier: carrier, TrackingNo: trackingNo,
		Status: ShipmentStatusShipped, ShippedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	r.shipments[orderID] = shipment
	r.transitionOrder(&order, OrderStatusShipped, "订单已发货", "admin", actorID, now)
	order.ShippedAt = timePointer(now)
	r.orders[orderID] = order
	return cloneOrder(r.attachOrderDetails(order)), nil
}

func (r *MemoryRepository) ConfirmOrder(_ context.Context, userID uint64, orderID string, now time.Time) (Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, exists := r.orders[orderID]
	if !exists || order.UserID != userID {
		return Order{}, NewError(CodeNotFound, "订单不存在", nil)
	}
	if order.Status == OrderStatusCompleted {
		return cloneOrder(r.attachOrderDetails(order)), nil
	}
	if !CanTransitionOrder(order.Status, OrderStatusCompleted) {
		return Order{}, NewError(CodeInvalidTransition, "当前订单不能确认收货", nil)
	}
	shipment := r.shipments[orderID]
	shipment.Status = ShipmentStatusReceived
	shipment.DeliveredAt = timePointer(now)
	shipment.UpdatedAt = now
	r.shipments[orderID] = shipment
	r.transitionOrder(&order, OrderStatusCompleted, "用户确认收货", "user", userID, now)
	order.CompletedAt = timePointer(now)
	r.orders[orderID] = order
	return cloneOrder(r.attachOrderDetails(order)), nil
}

func (r *MemoryRepository) RefundOrder(_ context.Context, userID uint64, orderID, refundNo, reason string, now time.Time) (Refund, Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, exists := r.orders[orderID]
	if !exists || order.UserID != userID {
		return Refund{}, Order{}, NewError(CodeNotFound, "订单不存在", nil)
	}
	if existingNo, exists := r.refundByOrder[orderID]; exists {
		refund := r.refunds[existingNo]
		return refund, cloneOrder(r.attachOrderDetails(order)), nil
	}
	if !CanTransitionOrder(order.Status, OrderStatusRefundPending) {
		return Refund{}, Order{}, NewError(CodeInvalidTransition, "当前订单不能退款", nil)
	}
	paymentNo, exists := r.paymentByOrder[orderID]
	if !exists || r.payments[paymentNo].Status != PaymentStatusSucceeded {
		return Refund{}, Order{}, NewError(CodeInvalidTransition, "订单没有成功支付记录", nil)
	}
	r.transitionOrder(&order, OrderStatusRefundPending, "申请退款: "+reason, "user", userID, now)
	for index, reservation := range r.reservations[orderID] {
		if reservation.Status != ReservationStatusConfirmed {
			continue
		}
		sku := r.skus[reservation.SKUID]
		sku.AvailableStock += reservation.Quantity
		sku.UpdatedAt = now
		r.skus[reservation.SKUID] = sku
		reservation.Status = ReservationStatusReleased
		reservation.UpdatedAt = now
		r.reservations[orderID][index] = reservation
	}
	refund := Refund{
		ID: r.next(), RefundNo: refundNo, OrderID: orderID, PaymentNo: paymentNo,
		AmountCents: order.TotalAmountCents, Reason: reason, Status: RefundStatusSucceeded,
		CreatedAt: now, CompletedAt: timePointer(now),
	}
	r.refunds[refundNo] = refund
	r.refundByOrder[orderID] = refundNo
	r.transitionOrder(&order, OrderStatusRefunded, "Mock 退款成功", "payment", 0, now)
	order.RefundedAt = timePointer(now)
	r.orders[orderID] = order
	return refund, cloneOrder(r.attachOrderDetails(order)), nil
}

func (r *MemoryRepository) transitionOrder(order *Order, to, reason, actorType string, actorID uint64, now time.Time) {
	from := order.Status
	order.Status = to
	order.UpdatedAt = now
	order.StatusHistory = append(order.StatusHistory, OrderStatus{
		ID: r.next(), OrderID: order.OrderID, FromStatus: from, ToStatus: to,
		Reason: reason, ActorType: actorType, ActorID: actorID, CreatedAt: now,
	})
}

func (r *MemoryRepository) releaseReservations(orderID, status string, now time.Time) {
	for index, reservation := range r.reservations[orderID] {
		if reservation.Status != ReservationStatusReserved {
			continue
		}
		sku := r.skus[reservation.SKUID]
		sku.AvailableStock += reservation.Quantity
		sku.ReservedStock -= reservation.Quantity
		sku.UpdatedAt = now
		r.skus[reservation.SKUID] = sku
		reservation.Status = status
		reservation.UpdatedAt = now
		r.reservations[orderID][index] = reservation
	}
}

func (r *MemoryRepository) attachOrderDetails(order Order) Order {
	if paymentNo := r.paymentByOrder[order.OrderID]; paymentNo != "" {
		payment := r.payments[paymentNo]
		order.Payment = &payment
	}
	if shipment, exists := r.shipments[order.OrderID]; exists {
		order.Shipment = &shipment
	}
	return order
}

func cloneOrder(order Order) Order {
	order.Items = append([]OrderItem(nil), order.Items...)
	order.StatusHistory = append([]OrderStatus(nil), order.StatusHistory...)
	if order.Payment != nil {
		payment := *order.Payment
		order.Payment = &payment
	}
	if order.Shipment != nil {
		shipment := *order.Shipment
		order.Shipment = &shipment
	}
	return order
}

func timePointer(value time.Time) *time.Time {
	copy := value
	return &copy
}

var _ Repository = (*MemoryRepository)(nil)

// FindUserIDByEmail 仅用于验收测试构建认证上下文。
func (r *MemoryRepository) FindUserIDByEmail(email string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.userByEmail[strings.ToLower(strings.TrimSpace(email))]
}
