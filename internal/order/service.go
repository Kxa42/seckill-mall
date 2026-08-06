package order

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const defaultRPCTimeout = 2 * time.Second

// Service 编排订单创建、库存补偿和订单生命周期，不直接访问下游数据库。
type Service struct {
	repository Repository
	catalog    CatalogClient
	identity   IdentityClient
	inventory  InventoryClient
	orderTTL   time.Duration
	rpcTimeout time.Duration
	now        func() time.Time
}

func NewService(repository Repository, catalog CatalogClient, identity IdentityClient, inventory InventoryClient, orderTTL time.Duration) (*Service, error) {
	if repository == nil || catalog == nil || identity == nil || inventory == nil {
		return nil, fmt.Errorf("order service dependencies are required")
	}
	if orderTTL <= 0 {
		return nil, fmt.Errorf("order ttl must be positive")
	}
	return &Service{repository: repository, catalog: catalog, identity: identity, inventory: inventory, orderTTL: orderTTL, rpcTimeout: defaultRPCTimeout, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Service) SetRPCTimeout(timeout time.Duration) {
	if timeout > 0 {
		s.rpcTimeout = timeout
	}
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (result Order, reused bool, resultErr error) {
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if err := validateCreate(command); err != nil {
		return Order{}, false, err
	}
	digest := requestDigest(command)
	if existing, found, err := s.repository.FindByIdempotency(ctx, command.UserID, command.IdempotencyKey); err != nil {
		return Order{}, false, err
	} else if found {
		if existing.RequestDigest != digest {
			return Order{}, false, NewError(CodeConflict, "Idempotency-Key 对应的请求参数不一致", nil)
		}
		intent, intentFound, intentErr := s.repository.FindCreateIntent(ctx, command.UserID, command.IdempotencyKey)
		if intentErr != nil {
			return Order{}, false, intentErr
		}
		if intentFound {
			if intent.RequestDigest != digest {
				return Order{}, false, NewError(CodeConflict, "Idempotency-Key 对应的创建意图参数不一致", nil)
			}
			intent.Status = CreateIntentDone
			intent.LastError = ""
			intent.UpdatedAt = s.now()
			if err := s.repository.UpdateCreateIntent(ctx, intent); err != nil {
				return Order{}, false, err
			}
		}
		return existing, true, nil
	}
	existingIntent, intentFound, err := s.repository.FindCreateIntent(ctx, command.UserID, command.IdempotencyKey)
	if err != nil {
		return Order{}, false, err
	} else if intentFound && existingIntent.RequestDigest != digest {
		return Order{}, false, NewError(CodeConflict, "Idempotency-Key 对应的创建意图参数不一致", nil)
	}
	if intentFound && existingIntent.Attempts >= maxCreateIntentAttempts {
		return Order{}, false, NewError(CodeUnavailable, "订单创建意图已达到重试上限", nil)
	}

	now := s.now()
	orderID := deterministicOrderID(command.UserID, command.IdempotencyKey, digest)
	payload, err := json.Marshal(command)
	if err != nil {
		return Order{}, false, err
	}
	attempts := 0
	if intentFound {
		attempts = existingIntent.Attempts
	}
	intent := CreateIntent{IntentID: "intent_" + strings.TrimPrefix(orderID, "ord_"), UserID: command.UserID, IdempotencyKey: command.IdempotencyKey, RequestDigest: digest, OrderID: orderID, Status: CreateIntentStarted, Attempts: attempts, Payload: string(payload), NextRetryAt: now, CreatedAt: now, UpdatedAt: now}
	if err := s.repository.SaveCreateIntent(ctx, intent); err != nil {
		return Order{}, false, err
	}
	defer func() {
		intent.UpdatedAt = s.now()
		if resultErr != nil {
			intent.Status = CreateIntentFailed
			intent.Attempts++
			intent.LastError = compactError(resultErr)
			intent.NextRetryAt = intent.UpdatedAt.Add(retryDelay(1))
		} else {
			intent.Status = CreateIntentDone
			intent.LastError = ""
		}
		_ = s.repository.UpdateCreateIntent(context.Background(), intent)
	}()
	items := append([]CreateItem(nil), command.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].SKUID < items[j].SKUID })
	itemIDs := make([]uint64, 0, len(items))
	for _, item := range items {
		itemIDs = append(itemIDs, item.SKUID)
	}

	ctx, cancel := context.WithTimeout(ctx, s.rpcTimeout)
	defer cancel()
	skus, err := s.catalog.GetSKUSnapshot(ctx, itemIDs)
	if err != nil {
		return Order{}, false, err
	}
	byID := make(map[uint64]SKU, len(skus))
	for _, sku := range skus {
		byID[sku.ID] = sku
	}
	address, err := s.identity.GetAddressSnapshot(ctx, command.UserID, command.AddressID)
	if err != nil {
		return Order{}, false, err
	}
	if address.ID != command.AddressID || address.UserID != command.UserID {
		return Order{}, false, NewError(CodeForbidden, "收货地址不属于当前用户", nil)
	}

	orderItems := make([]Item, 0, len(items))
	reservations := make([]Reservation, 0, len(items))
	var total int64
	for _, requested := range items {
		sku, ok := byID[requested.SKUID]
		if !ok || !sku.Active {
			return Order{}, false, NewError(CodeNotFound, "SKU 不存在或未上架", nil)
		}
		lineTotal, err := subtotal(sku.PriceCents, requested.Quantity)
		if err != nil {
			return Order{}, false, err
		}
		total, err = addAmount(total, lineTotal)
		if err != nil {
			return Order{}, false, err
		}
		reservationID := fmt.Sprintf("res_%s_%d", orderID, requested.SKUID)
		reserveCommand := ReserveCommand{ReservationID: reservationID, OrderID: orderID, UserID: command.UserID, ActivityID: command.ActivityID, SKUID: requested.SKUID, Quantity: requested.Quantity, Mode: command.OrderType}
		var reservation Reservation
		if command.OrderType == OrderTypeSeckill {
			reservation, err = s.inventory.AdmitSeckill(ctx, reserveCommand)
		} else {
			reservation, err = s.inventory.Reserve(ctx, reserveCommand)
		}
		if err != nil {
			s.compensateReservations(ctx, orderID, reservations)
			return Order{}, false, err
		}
		reservations = append(reservations, reservation)
		orderItems = append(orderItems, Item{SKUID: sku.ID, SKUCode: sku.Code, SKUName: sku.Name, UnitPriceCents: sku.PriceCents, Quantity: requested.Quantity, SubtotalCents: lineTotal, ReservationID: reservation.ReservationID})
	}

	order := Order{OrderID: orderID, UserID: command.UserID, AddressID: command.AddressID, OrderType: command.OrderType, Status: StatusPendingPayment, TotalAmountCents: total, IdempotencyKey: command.IdempotencyKey, RequestDigest: digest, AddressSnapshot: address, Items: orderItems, ExpiresAt: now.Add(s.orderTTL), CreatedAt: now, UpdatedAt: now, StatusHistory: []StatusHistory{{ToStatus: StatusPendingPayment, Reason: "创建订单", ActorType: "user", ActorID: command.UserID, CreatedAt: now}}}
	created, reused, err := s.repository.Create(ctx, order)
	if err != nil {
		s.compensateReservations(ctx, orderID, reservations)
		return Order{}, false, err
	}
	if reused {
		return created, true, nil
	}
	return created, false, nil
}

// RecoverCreateIntents 恢复进程中断或下游暂时失败的订单创建意图。
// 重试仍复用 Create 的确定性订单号、reservation ID 和幂等校验，不新增另一套创建路径。
func (s *Service) RecoverCreateIntents(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	intents, err := s.repository.ListPendingCreateIntents(ctx, s.now(), limit)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, intent := range intents {
		var command CreateCommand
		if err := json.Unmarshal([]byte(intent.Payload), &command); err != nil || command.UserID != intent.UserID || command.IdempotencyKey != intent.IdempotencyKey || requestDigest(command) != intent.RequestDigest {
			intent.Status = CreateIntentFailed
			intent.Attempts = maxCreateIntentAttempts
			intent.LastError = "创建意图载荷校验失败"
			intent.UpdatedAt = s.now()
			if updateErr := s.repository.UpdateCreateIntent(ctx, intent); updateErr != nil {
				return recovered, updateErr
			}
			continue
		}
		if _, _, err := s.Create(ctx, command); err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

func (s *Service) Get(ctx context.Context, userID uint64, orderID string) (Order, error) {
	if userID == 0 || strings.TrimSpace(orderID) == "" {
		return Order{}, NewError(CodeValidation, "用户和订单号不能为空", nil)
	}
	return s.repository.Get(ctx, userID, strings.TrimSpace(orderID))
}

func (s *Service) List(ctx context.Context, userID uint64, offset, limit int) ([]Order, int64, error) {
	if userID == 0 {
		return nil, 0, NewError(CodeValidation, "用户不能为空", nil)
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repository.List(ctx, userID, offset, limit)
}

func (s *Service) Cancel(ctx context.Context, userID uint64, orderID, reason string) (Order, error) {
	if userID == 0 {
		return Order{}, NewError(CodeValidation, "用户不能为空", nil)
	}
	return s.cancel(ctx, userID, strings.TrimSpace(orderID), strings.TrimSpace(reason), "user", userID)
}

func (s *Service) Expire(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	now := s.now()
	values, err := s.repository.ListExpired(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, value := range values {
		if _, err := s.cancel(ctx, 0, value.OrderID, "支付超时", "system", 0); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// ConfirmPayment 确认支付并提交库存预占。支付单本身由过渡 Payment 模块保存，订单状态仍只由本服务转换。
func (s *Service) ConfirmPayment(ctx context.Context, userID uint64, orderID, paymentNo, callbackRef string) (Order, bool, error) {
	if userID == 0 || strings.TrimSpace(orderID) == "" || strings.TrimSpace(paymentNo) == "" || strings.TrimSpace(callbackRef) == "" {
		return Order{}, false, NewError(CodeValidation, "支付确认参数不能为空", nil)
	}
	order, err := s.repository.Get(ctx, userID, strings.TrimSpace(orderID))
	if err != nil {
		return Order{}, false, err
	}
	if order.Status == StatusPaid {
		return order, true, nil
	}
	if order.Status != StatusPendingPayment {
		return Order{}, false, NewError(CodeInvalidTransition, "当前订单不能完成支付", nil)
	}
	for _, item := range order.Items {
		if err := s.inventory.Confirm(ctx, item.ReservationID, order.OrderID); err != nil {
			s.saveOperation(ctx, Operation{
				ID:            operationID("confirm", order.OrderID, item.ReservationID),
				OrderID:       order.OrderID,
				Kind:          "confirm",
				ReservationID: item.ReservationID,
				Status:        OperationPending,
				Attempts:      1,
				LastError:     err.Error(),
				NextRetryAt:   s.now().Add(time.Second),
				CreatedAt:     s.now(),
				UpdatedAt:     s.now(),
			})
			return Order{}, false, err
		}
	}
	value, err := s.repository.Transition(ctx, order.OrderID, userID, StatusPaid, "支付成功: "+strings.TrimSpace(paymentNo), "payment", 0, s.now())
	if err != nil {
		return Order{}, false, err
	}
	return value, false, nil
}

// Ship 将发货状态转换委托给 Order Service，物流单据仍由履约过渡模块保存。
func (s *Service) Ship(ctx context.Context, actorID uint64, orderID, carrier, trackingNo string) (Order, error) {
	if actorID == 0 || strings.TrimSpace(orderID) == "" || strings.TrimSpace(carrier) == "" || strings.TrimSpace(trackingNo) == "" {
		return Order{}, NewError(CodeValidation, "发货参数无效", nil)
	}
	value, err := s.repository.Get(ctx, 0, strings.TrimSpace(orderID))
	if err != nil {
		return Order{}, err
	}
	if value.Status == StatusShipped {
		return value, nil
	}
	return s.repository.Transition(ctx, value.OrderID, 0, StatusShipped, "订单已发货: "+strings.TrimSpace(carrier)+"/"+strings.TrimSpace(trackingNo), "admin", actorID, s.now())
}

// ConfirmReceipt 将收货状态转换委托给 Order Service。
func (s *Service) ConfirmReceipt(ctx context.Context, userID uint64, orderID string) (Order, error) {
	if userID == 0 || strings.TrimSpace(orderID) == "" {
		return Order{}, NewError(CodeValidation, "收货参数无效", nil)
	}
	return s.repository.Transition(ctx, strings.TrimSpace(orderID), userID, StatusCompleted, "用户确认收货", "user", userID, s.now())
}

// Refund 将订单置为已退款。退款单和退款渠道仍由过渡 Payment 模块保存。
func (s *Service) Refund(ctx context.Context, userID uint64, orderID, refundNo, reason string) (Order, error) {
	if userID == 0 || strings.TrimSpace(orderID) == "" || strings.TrimSpace(refundNo) == "" || strings.TrimSpace(reason) == "" {
		return Order{}, NewError(CodeValidation, "退款参数无效", nil)
	}
	value, err := s.repository.Get(ctx, userID, strings.TrimSpace(orderID))
	if err != nil {
		return Order{}, err
	}
	if value.Status == StatusRefunded {
		return value, nil
	}
	if value.Status == StatusPaid || value.Status == StatusShipped {
		if _, err := s.repository.Transition(ctx, value.OrderID, userID, StatusRefundPending, "申请退款: "+strings.TrimSpace(reason), "user", userID, s.now()); err != nil {
			return Order{}, err
		}
		value.Status = StatusRefundPending
	}
	if value.Status == StatusRefundPending {
		if err := s.restockItems(ctx, value); err != nil {
			return Order{}, err
		}
		return s.repository.Transition(ctx, value.OrderID, userID, StatusRefunded, "退款完成: "+strings.TrimSpace(refundNo), "payment", 0, s.now())
	}
	return Order{}, NewError(CodeInvalidTransition, "当前订单不能退款", nil)
}

// RetryOperations 重放持久化的幂等补偿操作，并对失败项执行有限退避。
func (s *Service) RetryOperations(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	now := s.now()
	operations, err := s.repository.ListPendingOperations(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	completed := 0
	for _, operation := range operations {
		if operation.Attempts >= maxOperationAttempts {
			operation.Status = OperationFailed
			operation.LastError = "超过最大重试次数"
			operation.UpdatedAt = now
			if err := s.repository.UpdateOperation(ctx, operation); err != nil {
				return completed, err
			}
			continue
		}
		if operation.Kind != "release" && operation.Kind != "confirm" && operation.Kind != "restock" {
			operation.Attempts++
			operation.LastError = "unsupported operation kind"
			operation.NextRetryAt = now.Add(retryDelay(operation.Attempts))
			operation.UpdatedAt = now
			if err := s.repository.UpdateOperation(ctx, operation); err != nil {
				return completed, err
			}
			continue
		}
		var err error
		if operation.Kind == "confirm" {
			err = s.inventory.Confirm(ctx, operation.ReservationID, operation.OrderID)
		} else if operation.Kind == "release" {
			err = s.inventory.Release(ctx, operation.ReservationID, operation.OrderID)
		} else {
			err = s.inventory.Restock(ctx, operation.ReservationID, operation.OrderID)
		}
		operation.Attempts++
		operation.UpdatedAt = now
		if err == nil {
			operation.Status = OperationDone
			operation.LastError = ""
			completed++
		} else {
			operation.LastError = err.Error()
			operation.NextRetryAt = now.Add(retryDelay(operation.Attempts))
		}
		if err := s.repository.UpdateOperation(ctx, operation); err != nil {
			return completed, err
		}
		if err == nil {
			if operation.Kind == "confirm" {
				if finalizeErr := s.finalizePendingPayment(ctx, operation.OrderID); finalizeErr != nil {
					return completed, finalizeErr
				}
			}
			if operation.Kind == "restock" {
				if finalizeErr := s.finalizePendingRefund(ctx, operation.OrderID); finalizeErr != nil {
					return completed, finalizeErr
				}
			}
			if operation.Kind == "release" {
				if finalizeErr := s.finalizePendingCancellation(ctx, operation.OrderID); finalizeErr != nil {
					return completed, finalizeErr
				}
			}
		}
	}
	return completed, nil
}

func (s *Service) cancel(ctx context.Context, userID uint64, orderID, reason, actorType string, actorID uint64) (Order, error) {
	if orderID == "" {
		return Order{}, NewError(CodeValidation, "订单号不能为空", nil)
	}
	order, err := s.repository.Get(ctx, userID, orderID)
	if err != nil {
		return Order{}, err
	}
	if order.Status == StatusCanceled {
		return order, nil
	}
	for _, item := range order.Items {
		if err := s.inventory.Release(ctx, item.ReservationID, order.OrderID); err != nil {
			now := s.now()
			_ = s.repository.SaveOperation(ctx, Operation{ID: operationID("release", order.OrderID, item.ReservationID), OrderID: order.OrderID, Kind: "release", ReservationID: item.ReservationID, Status: OperationPending, Attempts: 1, LastError: err.Error(), NextRetryAt: now.Add(time.Second), CreatedAt: now, UpdatedAt: now})
			return Order{}, err
		}
	}
	if reason == "" {
		reason = "用户取消"
	}
	return s.repository.Transition(ctx, order.OrderID, userID, StatusCanceled, reason, actorType, actorID, s.now())
}

func (s *Service) finalizePendingPayment(ctx context.Context, orderID string) error {
	value, err := s.repository.Get(ctx, 0, orderID)
	if err != nil || value.Status != StatusPendingPayment {
		return err
	}
	for _, item := range value.Items {
		if err := s.inventory.Confirm(ctx, item.ReservationID, value.OrderID); err != nil {
			return err
		}
	}
	_, err = s.repository.Transition(ctx, value.OrderID, 0, StatusPaid, "支付成功补偿完成", "payment", 0, s.now())
	return err
}

func (s *Service) finalizePendingCancellation(ctx context.Context, orderID string) error {
	value, err := s.repository.Get(ctx, 0, orderID)
	if err != nil {
		if ErrorCode(err) == CodeNotFound {
			// 创建失败补偿没有本地订单，只需要完成 reservation 释放。
			return nil
		}
		return err
	}
	if value.Status != StatusPendingPayment {
		return nil
	}
	for _, item := range value.Items {
		if err := s.inventory.Release(ctx, item.ReservationID, value.OrderID); err != nil {
			now := s.now()
			s.saveOperation(ctx, Operation{ID: operationID("release", value.OrderID, item.ReservationID), OrderID: value.OrderID, Kind: "release", ReservationID: item.ReservationID, Status: OperationPending, Attempts: 1, LastError: err.Error(), NextRetryAt: now.Add(time.Second), CreatedAt: now, UpdatedAt: now})
			return err
		}
	}
	_, err = s.repository.Transition(ctx, value.OrderID, 0, StatusCanceled, "库存释放补偿完成", "system", 0, s.now())
	return err
}

func (s *Service) finalizePendingRefund(ctx context.Context, orderID string) error {
	value, err := s.repository.Get(ctx, 0, orderID)
	if err != nil || value.Status != StatusRefundPending {
		return err
	}
	if err := s.restockItems(ctx, value); err != nil {
		return err
	}
	_, err = s.repository.Transition(ctx, value.OrderID, 0, StatusRefunded, "退款补偿完成", "payment", 0, s.now())
	return err
}

func (s *Service) restockItems(ctx context.Context, value Order) error {
	for _, item := range value.Items {
		if err := s.inventory.Restock(ctx, item.ReservationID, value.OrderID); err != nil {
			now := s.now()
			s.saveOperation(ctx, Operation{
				ID:            operationID("restock", value.OrderID, item.ReservationID),
				OrderID:       value.OrderID,
				Kind:          "restock",
				ReservationID: item.ReservationID,
				Status:        OperationPending,
				Attempts:      1,
				LastError:     err.Error(),
				NextRetryAt:   now.Add(time.Second),
				CreatedAt:     now,
				UpdatedAt:     now,
			})
			return err
		}
		now := s.now()
		s.saveOperation(ctx, Operation{
			ID:            operationID("restock", value.OrderID, item.ReservationID),
			OrderID:       value.OrderID,
			Kind:          "restock",
			ReservationID: item.ReservationID,
			Status:        OperationDone,
			Attempts:      1,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}
	return nil
}

func (s *Service) compensateReservations(ctx context.Context, orderID string, reservations []Reservation) {
	for index := len(reservations) - 1; index >= 0; index-- {
		reservation := reservations[index]
		if err := s.inventory.Release(ctx, reservation.ReservationID, orderID); err != nil {
			now := s.now()
			_ = s.repository.SaveOperation(ctx, Operation{ID: operationID("release", orderID, reservation.ReservationID), OrderID: orderID, Kind: "release", ReservationID: reservation.ReservationID, Status: OperationPending, Attempts: 1, LastError: err.Error(), NextRetryAt: now.Add(time.Second), CreatedAt: now, UpdatedAt: now})
		}
	}
}

func (s *Service) saveOperation(ctx context.Context, operation Operation) {
	if err := s.repository.SaveOperation(ctx, operation); err != nil {
		// 主流程已经返回下游错误，补偿记录失败只能交由外层告警和人工处理。
		return
	}
}

func requestDigest(command CreateCommand) string {
	items := append([]CreateItem(nil), command.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].SKUID < items[j].SKUID })
	hash := sha256.New()
	fmt.Fprintf(hash, "%d|%d|%s|%d|", command.UserID, command.AddressID, command.OrderType, command.ActivityID)
	for _, item := range items {
		fmt.Fprintf(hash, "%d:%d;", item.SKUID, item.Quantity)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func deterministicOrderID(userID uint64, idempotencyKey, digest string) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%d|%s|%s", userID, strings.TrimSpace(idempotencyKey), digest)
	return "ord_" + hex.EncodeToString(hash.Sum(nil))[:32]
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 255 {
		return message[:255]
	}
	return message
}
