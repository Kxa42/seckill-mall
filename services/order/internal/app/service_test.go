package order

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCatalog struct {
	items []SKU
}

func (f *fakeCatalog) GetSKUSnapshot(_ context.Context, ids []uint64) ([]SKU, error) {
	byID := make(map[uint64]SKU, len(f.items))
	for _, item := range f.items {
		byID[item.ID] = item
	}
	result := make([]SKU, 0, len(ids))
	for _, id := range ids {
		item, ok := byID[id]
		if !ok {
			return nil, NewError(CodeNotFound, "SKU 不存在", nil)
		}
		result = append(result, item)
	}
	return result, nil
}

type fakeIdentity struct{}

func (fakeIdentity) GetAddressSnapshot(_ context.Context, userID, addressID uint64) (Address, error) {
	if userID != 9 || addressID != 7 {
		return Address{}, NewError(CodeNotFound, "地址不存在", nil)
	}
	return Address{ID: addressID, UserID: userID, Recipient: "测试用户", Phone: "13800000000", Detail: "测试地址"}, nil
}

type fakeInventory struct {
	reservations  map[string]Reservation
	reserveCalls  int
	admitCalls    int
	releaseCalls  int
	confirmCalls  int
	restockCalls  int
	failReserveID string
	failReleaseID string
	failConfirmID string
	failRestockID string
}

func newFakeInventory() *fakeInventory {
	return &fakeInventory{reservations: make(map[string]Reservation)}
}

func (f *fakeInventory) Reserve(_ context.Context, command ReserveCommand) (Reservation, error) {
	f.reserveCalls++
	if command.ReservationID == f.failReserveID {
		return Reservation{}, NewError(CodeOutOfStock, "库存不足", nil)
	}
	if existing, ok := f.reservations[command.ReservationID]; ok {
		return existing, nil
	}
	value := Reservation{ReservationID: command.ReservationID, OrderID: command.OrderID, SKUID: command.SKUID, Quantity: command.Quantity, Status: "reserved"}
	f.reservations[value.ReservationID] = value
	return value, nil
}

func (f *fakeInventory) AdmitSeckill(_ context.Context, command ReserveCommand) (Reservation, error) {
	f.admitCalls++
	if command.ReservationID == f.failReserveID {
		return Reservation{}, NewError(CodeOutOfStock, "库存不足", nil)
	}
	if existing, ok := f.reservations[command.ReservationID]; ok {
		return existing, nil
	}
	value := Reservation{ReservationID: "admit_" + command.ReservationID, OrderID: command.OrderID, SKUID: command.SKUID, Quantity: command.Quantity, Status: "reserved"}
	f.reservations[value.ReservationID] = value
	return value, nil
}

func (f *fakeInventory) Confirm(_ context.Context, reservationID, _ string) error {
	f.confirmCalls++
	if reservationID == f.failConfirmID {
		return NewError(CodeUnavailable, "confirm failed", nil)
	}
	value, ok := f.reservations[reservationID]
	if !ok {
		return errors.New("reservation not found")
	}
	value.Status = "confirmed"
	f.reservations[reservationID] = value
	return nil
}

func (f *fakeInventory) Release(_ context.Context, reservationID, _ string) error {
	f.releaseCalls++
	if reservationID == f.failReleaseID {
		return errors.New("release failed")
	}
	value, ok := f.reservations[reservationID]
	if !ok {
		return errors.New("reservation not found")
	}
	value.Status = "released"
	f.reservations[reservationID] = value
	return nil
}

func (f *fakeInventory) Restock(_ context.Context, reservationID, _ string) error {
	f.restockCalls++
	if reservationID == f.failRestockID {
		return NewError(CodeUnavailable, "restock failed", nil)
	}
	value, ok := f.reservations[reservationID]
	if !ok {
		return errors.New("reservation not found")
	}
	if value.Status == "restocked" {
		return nil
	}
	if value.Status != "confirmed" {
		return errors.New("reservation is not confirmed")
	}
	value.Status = "restocked"
	f.reservations[reservationID] = value
	return nil
}

func newTestService(t *testing.T, inventory *fakeInventory) (*Service, *MemoryRepository) {
	t.Helper()
	repository := NewMemoryRepository()
	service, err := NewService(repository, &fakeCatalog{items: []SKU{{ID: 1, Code: "SKU-1", Name: "商品1", PriceCents: 1000, Active: true}, {ID: 2, Code: "SKU-2", Name: "商品2", PriceCents: 2500, Active: true}}}, fakeIdentity{}, inventory, time.Minute)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service, repository
}

func createCommand(orderType string) CreateCommand {
	return CreateCommand{UserID: 9, AddressID: 7, OrderType: orderType, ActivityID: 100, IdempotencyKey: "checkout-1", Items: []CreateItem{{SKUID: 1, Quantity: 2}}}
}

func TestCreateIsIdempotentAndUsesCatalogSnapshot(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	first, reused, err := service.Create(context.Background(), createCommand(OrderTypeNormal))
	if err != nil || reused {
		t.Fatalf("first Create() = %+v, reused=%v, err=%v", first, reused, err)
	}
	if first.TotalAmountCents != 2000 || len(first.Items) != 1 || first.Items[0].ReservationID == "" {
		t.Fatalf("unexpected created order: %+v", first)
	}
	second, reused, err := service.Create(context.Background(), createCommand(OrderTypeNormal))
	if err != nil || !reused || second.OrderID != first.OrderID {
		t.Fatalf("duplicate Create() = %+v, reused=%v, err=%v", second, reused, err)
	}
	if inventory.reserveCalls != 1 {
		t.Fatalf("duplicate request reserve calls = %d, want 1", inventory.reserveCalls)
	}
	intent, ok := repository.CreateIntent(9, "checkout-1")
	if !ok || intent.Status != CreateIntentDone || intent.OrderID != first.OrderID {
		t.Fatalf("create intent = %+v, exists=%v", intent, ok)
	}

	conflict := createCommand(OrderTypeNormal)
	conflict.Items[0].Quantity = 3
	if _, _, err := service.Create(context.Background(), conflict); ErrorCode(err) != CodeConflict {
		t.Fatalf("same key with different payload error = %v, want conflict", err)
	}
}

func TestDistinctIdempotencyKeysCreateDistinctOrdersForSamePayload(t *testing.T) {
	inventory := newFakeInventory()
	service, _ := newTestService(t, inventory)
	firstCommand := createCommand(OrderTypeNormal)
	first, _, err := service.Create(context.Background(), firstCommand)
	if err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	secondCommand := firstCommand
	secondCommand.IdempotencyKey = "checkout-2"
	second, reused, err := service.Create(context.Background(), secondCommand)
	if err != nil || reused {
		t.Fatalf("Create(second) = %+v, reused=%v, err=%v", second, reused, err)
	}
	if first.OrderID == second.OrderID || first.Items[0].ReservationID == second.Items[0].ReservationID {
		t.Fatalf("distinct idempotency keys reused IDs: first=%+v second=%+v", first, second)
	}
	if inventory.reserveCalls != 2 {
		t.Fatalf("reserve calls = %d, want 2", inventory.reserveCalls)
	}
}

func TestRecoverCreateIntentsReusesDeterministicOrderAndReservationIDs(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	command := createCommand(OrderTypeNormal)
	digest := requestDigest(command)
	orderID := deterministicOrderID(command.UserID, command.IdempotencyKey, digest)
	inventory.failReserveID = "res_" + orderID + "_1"
	if _, _, err := service.Create(context.Background(), command); ErrorCode(err) != CodeOutOfStock {
		t.Fatalf("Create() error = %v, want out of stock", err)
	}
	intent, ok := repository.CreateIntent(command.UserID, command.IdempotencyKey)
	if !ok || intent.Status != CreateIntentFailed || intent.Attempts != 1 {
		t.Fatalf("failed create intent = %+v, exists=%v", intent, ok)
	}
	inventory.failReserveID = ""
	service.now = func() time.Time { return now.Add(2 * time.Second) }
	count, err := service.RecoverCreateIntents(context.Background(), 10)
	if err != nil || count != 1 {
		t.Fatalf("RecoverCreateIntents() count=%d, err=%v", count, err)
	}
	created, reused, err := service.Create(context.Background(), command)
	if err != nil || !reused {
		t.Fatalf("recovered Create() = %+v, reused=%v, err=%v", created, reused, err)
	}
	if created.OrderID != orderID || created.Items[0].ReservationID != "res_"+orderID+"_1" {
		t.Fatalf("recovered IDs = order=%q reservation=%q", created.OrderID, created.Items[0].ReservationID)
	}
	intent, ok = repository.CreateIntent(command.UserID, command.IdempotencyKey)
	if !ok || intent.Status != CreateIntentDone || intent.Attempts != 1 {
		t.Fatalf("recovered create intent = %+v, exists=%v", intent, ok)
	}
}

func TestIdempotentExistingOrderCompletesStaleCreateIntent(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	command := createCommand(OrderTypeNormal)
	created, _, err := service.Create(context.Background(), command)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	intent, ok := repository.CreateIntent(command.UserID, command.IdempotencyKey)
	if !ok {
		t.Fatal("create intent not found")
	}
	intent.Status = CreateIntentStarted
	intent.NextRetryAt = time.Time{}
	if err := repository.UpdateCreateIntent(context.Background(), intent); err != nil {
		t.Fatalf("UpdateCreateIntent() error = %v", err)
	}
	value, reused, err := service.Create(context.Background(), command)
	if err != nil || !reused || value.OrderID != created.OrderID {
		t.Fatalf("Create(existing) = %+v, reused=%v, err=%v", value, reused, err)
	}
	intent, ok = repository.CreateIntent(command.UserID, command.IdempotencyKey)
	if !ok || intent.Status != CreateIntentDone {
		t.Fatalf("stale intent after idempotent hit = %+v, exists=%v", intent, ok)
	}
}

func TestSeckillUsesOneAdmissionPathAndBindsOrder(t *testing.T) {
	inventory := newFakeInventory()
	service, _ := newTestService(t, inventory)
	created, _, err := service.Create(context.Background(), createCommand(OrderTypeSeckill))
	if err != nil {
		t.Fatalf("Create(seckill) error = %v", err)
	}
	if inventory.admitCalls != 1 || inventory.reserveCalls != 0 {
		t.Fatalf("seckill inventory calls admit=%d reserve=%d, want admit=1 reserve=0", inventory.admitCalls, inventory.reserveCalls)
	}
	if got := inventory.reservations[created.Items[0].ReservationID].OrderID; got != created.OrderID {
		t.Fatalf("seckill reservation order_id = %q, want %q", got, created.OrderID)
	}
}

func TestCreateCompensatesAlreadyReservedItems(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	command := createCommand(OrderTypeNormal)
	command.Items = []CreateItem{{SKUID: 1, Quantity: 1}, {SKUID: 2, Quantity: 1}}
	digest := requestDigest(command)
	orderID := deterministicOrderID(command.UserID, command.IdempotencyKey, digest)
	inventory.failReserveID = "res_" + orderID + "_2"
	if _, _, err := service.Create(context.Background(), command); ErrorCode(err) != CodeOutOfStock {
		t.Fatalf("Create(partial failure) error = %v, want out of stock", err)
	}
	if inventory.releaseCalls != 1 {
		t.Fatalf("compensation release calls = %d, want 1", inventory.releaseCalls)
	}
	if _, found, _ := repository.FindByIdempotency(context.Background(), 9, command.IdempotencyKey); found {
		t.Fatal("failed create must not persist an order")
	}
}

func TestCancelReleasesReservationBeforeStateTransition(t *testing.T) {
	inventory := newFakeInventory()
	service, _ := newTestService(t, inventory)
	created, _, err := service.Create(context.Background(), createCommand(OrderTypeNormal))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	canceled, err := service.Cancel(context.Background(), 9, created.OrderID, "不再需要")
	if err != nil || canceled.Status != StatusCanceled {
		t.Fatalf("Cancel() = %+v, err=%v", canceled, err)
	}
	if inventory.releaseCalls != 1 || inventory.reservations[created.Items[0].ReservationID].Status != "released" {
		t.Fatalf("reservation was not released before cancellation: %+v", inventory.reservations)
	}
	repeated, err := service.Cancel(context.Background(), 9, created.OrderID, "重复取消")
	if err != nil || repeated.Status != StatusCanceled || inventory.releaseCalls != 1 {
		t.Fatalf("repeated Cancel() = %+v, err=%v, releases=%d", repeated, err, inventory.releaseCalls)
	}
}

func TestExpireCancelsExpiredOrders(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	created, _, err := service.Create(context.Background(), createCommand(OrderTypeNormal))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	service.now = func() time.Time { return now.Add(2 * time.Minute) }
	count, err := service.Expire(context.Background(), 10)
	if err != nil || count != 1 {
		t.Fatalf("Expire() count=%d, err=%v", count, err)
	}
	value, err := repository.Get(context.Background(), 9, created.OrderID)
	if err != nil || value.Status != StatusCanceled {
		t.Fatalf("expired order = %+v, err=%v", value, err)
	}
}

func TestRetryOperationsRecoversFailedRelease(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	created, _, err := service.Create(context.Background(), createCommand(OrderTypeNormal))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	reservationID := created.Items[0].ReservationID
	inventory.failReleaseID = reservationID
	if _, err := service.Cancel(context.Background(), 9, created.OrderID, "测试失败"); err == nil {
		t.Fatal("Cancel() should report failed release")
	}
	operationKey := operationID("release", created.OrderID, reservationID)
	operation, ok := repository.Operation(operationKey)
	if !ok || operation.Status != OperationPending {
		t.Fatalf("pending operation = %+v, exists=%v", operation, ok)
	}
	inventory.failReleaseID = ""
	service.now = func() time.Time { return now.Add(2 * time.Second) }
	count, err := service.RetryOperations(context.Background(), 10)
	if err != nil || count != 1 {
		t.Fatalf("RetryOperations() count=%d, err=%v", count, err)
	}
	operation, _ = repository.Operation(operationKey)
	if operation.Status != OperationDone {
		t.Fatalf("retried operation status = %s, want done", operation.Status)
	}
	value, err := repository.Get(context.Background(), 9, created.OrderID)
	if err != nil || value.Status != StatusCanceled {
		t.Fatalf("order after release recovery = %+v, err=%v", value, err)
	}
}

func TestPaymentAndFulfillmentTransitionsAreIdempotent(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	created, _, err := service.Create(context.Background(), createCommand(OrderTypeNormal))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	paid, reused, err := service.ConfirmPayment(context.Background(), 9, created.OrderID, "pay-1", "callback-1")
	if err != nil || reused || paid.Status != StatusPaid || inventory.confirmCalls != 1 {
		t.Fatalf("ConfirmPayment() = %+v, reused=%v, err=%v, confirms=%d", paid, reused, err, inventory.confirmCalls)
	}
	paidAgain, reused, err := service.ConfirmPayment(context.Background(), 9, created.OrderID, "pay-1", "callback-1")
	if err != nil || !reused || paidAgain.Status != StatusPaid || inventory.confirmCalls != 1 {
		t.Fatalf("repeated ConfirmPayment() = %+v, reused=%v, err=%v, confirms=%d", paidAgain, reused, err, inventory.confirmCalls)
	}
	shipped, err := service.Ship(context.Background(), 99, created.OrderID, "SF", "SF-1")
	if err != nil || shipped.Status != StatusShipped {
		t.Fatalf("Ship() = %+v, err=%v", shipped, err)
	}
	completed, err := service.ConfirmReceipt(context.Background(), 9, created.OrderID)
	if err != nil || completed.Status != StatusCompleted {
		t.Fatalf("ConfirmReceipt() = %+v, err=%v", completed, err)
	}
	stored, err := repository.Get(context.Background(), 9, created.OrderID)
	if err != nil || stored.Status != StatusCompleted {
		t.Fatalf("stored lifecycle status = %+v, err=%v", stored, err)
	}
}

func TestRefundShippedOrderFinalizesRefund(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	created, _, err := service.Create(context.Background(), createCommand(OrderTypeNormal))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, _, err := service.ConfirmPayment(context.Background(), 9, created.OrderID, "pay-shipped-refund", "callback-shipped-refund"); err != nil {
		t.Fatalf("ConfirmPayment() error = %v", err)
	}
	shipped, err := service.Ship(context.Background(), 99, created.OrderID, "SF", "SF-1")
	if err != nil || shipped.Status != StatusShipped {
		t.Fatalf("Ship() = %+v, err=%v", shipped, err)
	}
	refunded, err := service.Refund(context.Background(), 9, created.OrderID, "refund-shipped", "已发货退款")
	if err != nil || refunded.Status != StatusRefunded {
		t.Fatalf("Refund(shipped) = %+v, err=%v", refunded, err)
	}
	reservation := inventory.reservations[created.Items[0].ReservationID]
	if reservation.Status != "restocked" || inventory.restockCalls != 1 {
		t.Fatalf("restock state = %+v, calls=%d", reservation, inventory.restockCalls)
	}
	stored, err := repository.Get(context.Background(), 9, created.OrderID)
	if err != nil || stored.Status != StatusRefunded {
		t.Fatalf("stored refund status = %+v, err=%v", stored, err)
	}
}

func TestRefundRestocksConfirmedReservationsBeforeCompleting(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	created, _, err := service.Create(context.Background(), createCommand(OrderTypeNormal))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, _, err := service.ConfirmPayment(context.Background(), 9, created.OrderID, "pay-refund", "callback-refund"); err != nil {
		t.Fatalf("ConfirmPayment() error = %v", err)
	}
	refunded, err := service.Refund(context.Background(), 9, created.OrderID, "refund-1", "改变主意")
	if err != nil || refunded.Status != StatusRefunded {
		t.Fatalf("Refund() = %+v, err=%v", refunded, err)
	}
	reservation := inventory.reservations[created.Items[0].ReservationID]
	if reservation.Status != "restocked" || inventory.restockCalls != 1 {
		t.Fatalf("restock state = %+v, calls=%d", reservation, inventory.restockCalls)
	}
	repeated, err := service.Refund(context.Background(), 9, created.OrderID, "refund-1", "重复退款")
	if err != nil || repeated.Status != StatusRefunded || inventory.restockCalls != 1 {
		t.Fatalf("repeated Refund() = %+v, err=%v, calls=%d", repeated, err, inventory.restockCalls)
	}
	stored, err := repository.Get(context.Background(), 9, created.OrderID)
	if err != nil || stored.Status != StatusRefunded {
		t.Fatalf("stored refund status = %+v, err=%v", stored, err)
	}
}

func TestRefundRestockFailureIsRetriedAndFinalizesRefund(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	created, _, err := service.Create(context.Background(), createCommand(OrderTypeNormal))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, _, err := service.ConfirmPayment(context.Background(), 9, created.OrderID, "pay-refund-retry", "callback-refund-retry"); err != nil {
		t.Fatalf("ConfirmPayment() error = %v", err)
	}
	inventory.failRestockID = created.Items[0].ReservationID
	if _, err := service.Refund(context.Background(), 9, created.OrderID, "refund-retry", "退款重试"); ErrorCode(err) != CodeUnavailable {
		t.Fatalf("Refund() error = %v, want unavailable", err)
	}
	value, err := repository.Get(context.Background(), 9, created.OrderID)
	if err != nil || value.Status != StatusRefundPending {
		t.Fatalf("pending refund order = %+v, err=%v", value, err)
	}
	operation, ok := repository.Operation(operationID("restock", created.OrderID, created.Items[0].ReservationID))
	if !ok || operation.Status != OperationPending {
		t.Fatalf("pending restock operation = %+v, exists=%v", operation, ok)
	}
	inventory.failRestockID = ""
	service.now = func() time.Time { return now.Add(2 * time.Second) }
	count, err := service.RetryOperations(context.Background(), 10)
	if err != nil || count != 1 {
		t.Fatalf("RetryOperations() count=%d, err=%v", count, err)
	}
	value, err = repository.Get(context.Background(), 9, created.OrderID)
	if err != nil || value.Status != StatusRefunded {
		t.Fatalf("finalized refund order = %+v, err=%v", value, err)
	}
}

func TestConfirmPaymentFailureLeavesRetryOperation(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	created, _, err := service.Create(context.Background(), createCommand(OrderTypeNormal))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	reservationID := created.Items[0].ReservationID
	inventory.failConfirmID = reservationID
	if _, _, err := service.ConfirmPayment(context.Background(), 9, created.OrderID, "pay-2", "callback-2"); ErrorCode(err) != CodeUnavailable {
		t.Fatalf("ConfirmPayment() error = %v, want unavailable", err)
	}
	operation, ok := repository.Operation(operationID("confirm", created.OrderID, reservationID))
	if !ok || operation.Status != OperationPending {
		t.Fatalf("confirm retry operation = %+v, exists=%v", operation, ok)
	}
}

func TestRetryOperationsStopsAfterMaximumAttempts(t *testing.T) {
	inventory := newFakeInventory()
	service, repository := newTestService(t, inventory)
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	created, _, err := service.Create(context.Background(), createCommand(OrderTypeNormal))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	reservationID := created.Items[0].ReservationID
	inventory.failReleaseID = reservationID
	if _, err := service.Cancel(context.Background(), 9, created.OrderID, "重试上限测试"); err == nil {
		t.Fatal("Cancel() should fail while release is unavailable")
	}
	for i := 0; i < maxOperationAttempts+1; i++ {
		now = now.Add(time.Hour)
		if _, err := service.RetryOperations(context.Background(), 10); err != nil {
			t.Fatalf("RetryOperations(%d) error = %v", i, err)
		}
	}
	operation, ok := repository.Operation(operationID("release", created.OrderID, reservationID))
	if !ok || operation.Status != OperationFailed {
		t.Fatalf("operation after max attempts = %+v, exists=%v", operation, ok)
	}
}
