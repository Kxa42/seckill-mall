package inventoryservice

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MemoryStore 是不依赖 Redis/MySQL 的并发安全库存实现，用于 Fake E2E。
type MemoryStore struct {
	mu            sync.Mutex
	stock         map[uint64]int32
	reservations  map[string]Reservation
	purchased     map[string]int32
	purchaseLimit int32
	now           func() time.Time
}

// NewMemoryStore 创建库存服务内存实现，stock 表示每个 SKU 的可用库存。
func NewMemoryStore(stock map[uint64]int32, purchaseLimit int32) *MemoryStore {
	copyStock := make(map[uint64]int32, len(stock))
	for skuID, quantity := range stock {
		copyStock[skuID] = quantity
	}
	if purchaseLimit <= 0 {
		purchaseLimit = 1
	}
	return &MemoryStore{stock: copyStock, reservations: make(map[string]Reservation), purchased: make(map[string]int32), purchaseLimit: purchaseLimit, now: func() time.Time { return time.Now().UTC() }}
}

func (s *MemoryStore) Reserve(_ context.Context, command ReserveCommand) (Reservation, error) {
	if err := validateReserveCommand(command); err != nil {
		return Reservation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.reservations[command.ReservationID]; ok {
		if !sameReservation(existing, command) {
			return Reservation{}, ErrConflict
		}
		return existing, nil
	}
	if s.stock[command.SKUID] < command.Quantity {
		return Reservation{}, ErrOutOfStock
	}
	now := s.now()
	reservation := Reservation{ReservationID: command.ReservationID, OrderID: command.OrderID, UserID: command.UserID, SKUID: command.SKUID, Quantity: command.Quantity, Status: ReservationReserved, Mode: command.Mode, CreatedAt: now, UpdatedAt: now}
	s.stock[command.SKUID] -= command.Quantity
	s.reservations[command.ReservationID] = reservation
	return reservation, nil
}

func (s *MemoryStore) Confirm(_ context.Context, reservationID, orderID string) (Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reservation, err := s.findReservation(reservationID, orderID)
	if err != nil {
		return Reservation{}, err
	}
	switch reservation.Status {
	case ReservationConfirmed:
		return reservation, nil
	case ReservationReserved:
		if reservation.OrderID == "" && orderID != "" {
			reservation.OrderID = orderID
		}
		reservation.Status = ReservationConfirmed
		reservation.UpdatedAt = s.now()
		s.reservations[reservationID] = reservation
		return reservation, nil
	default:
		return Reservation{}, ErrConflict
	}
}

func (s *MemoryStore) Release(_ context.Context, reservationID, orderID string) (Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reservation, err := s.findReservation(reservationID, orderID)
	if err != nil {
		return Reservation{}, err
	}
	switch reservation.Status {
	case ReservationReleased, ReservationExpired:
		return reservation, nil
	case ReservationReserved:
		s.stock[reservation.SKUID] += reservation.Quantity
		if reservation.Mode == "seckill" {
			purchaseKey := fmt.Sprintf("%d:%d:%d", reservation.ActivityID, reservation.UserID, reservation.SKUID)
			s.purchased[purchaseKey] -= reservation.Quantity
			if s.purchased[purchaseKey] <= 0 {
				delete(s.purchased, purchaseKey)
			}
		}
		reservation.Status = ReservationReleased
		reservation.UpdatedAt = s.now()
		s.reservations[reservationID] = reservation
		return reservation, nil
	default:
		return Reservation{}, ErrConflict
	}
}

// Restock 将已确认的库存 reservation 以幂等方式恢复到可用库存。
// 它与 Release 分离，避免支付前取消误恢复已确认库存。
func (s *MemoryStore) Restock(_ context.Context, reservationID, orderID string) (Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reservation, err := s.findReservation(reservationID, orderID)
	if err != nil {
		return Reservation{}, err
	}
	switch reservation.Status {
	case ReservationRestocked:
		return reservation, nil
	case ReservationConfirmed:
		s.stock[reservation.SKUID] += reservation.Quantity
		if reservation.Mode == "seckill" {
			purchaseKey := fmt.Sprintf("%d:%d:%d", reservation.ActivityID, reservation.UserID, reservation.SKUID)
			s.purchased[purchaseKey] -= reservation.Quantity
			if s.purchased[purchaseKey] <= 0 {
				delete(s.purchased, purchaseKey)
			}
		}
		reservation.Status = ReservationRestocked
		reservation.UpdatedAt = s.now()
		s.reservations[reservationID] = reservation
		return reservation, nil
	default:
		return Reservation{}, ErrConflict
	}
}

func (s *MemoryStore) AdmitSeckill(_ context.Context, command SeckillAdmissionCommand) (Reservation, error) {
	if !validOpaqueID(command.RequestID) || command.ActivityID == 0 || command.UserID == 0 || command.SKUID == 0 || command.Quantity <= 0 {
		return Reservation{}, ErrInvalidRequest
	}
	reservationID := "admit_" + command.RequestID
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.reservations[reservationID]; ok {
		if existing.ActivityID != command.ActivityID || existing.UserID != command.UserID || existing.SKUID != command.SKUID || existing.Quantity != command.Quantity || (command.OrderID != "" && existing.OrderID != command.OrderID) {
			return Reservation{}, ErrConflict
		}
		return existing, nil
	}
	purchaseKey := fmt.Sprintf("%d:%d:%d", command.ActivityID, command.UserID, command.SKUID)
	if s.purchased[purchaseKey]+command.Quantity > s.purchaseLimit {
		return Reservation{}, ErrPurchaseLimit
	}
	if s.stock[command.SKUID] < command.Quantity {
		return Reservation{}, ErrOutOfStock
	}
	now := s.now()
	reservation := Reservation{ReservationID: reservationID, OrderID: command.OrderID, UserID: command.UserID, ActivityID: command.ActivityID, SKUID: command.SKUID, Quantity: command.Quantity, Status: ReservationReserved, Mode: "seckill", CreatedAt: now, UpdatedAt: now}
	s.stock[command.SKUID] -= command.Quantity
	s.purchased[purchaseKey] += command.Quantity
	s.reservations[reservationID] = reservation
	return reservation, nil
}

func (s *MemoryStore) findReservation(reservationID, orderID string) (Reservation, error) {
	if !validOpaqueID(reservationID) {
		return Reservation{}, ErrInvalidRequest
	}
	reservation, ok := s.reservations[reservationID]
	if !ok {
		return Reservation{}, ErrNotFound
	}
	if orderID != "" && reservation.OrderID != "" && reservation.OrderID != orderID {
		return Reservation{}, ErrConflict
	}
	return reservation, nil
}

func validateReserveCommand(command ReserveCommand) error {
	if !validOpaqueID(command.ReservationID) || !validOpaqueID(command.OrderID) || command.UserID == 0 || command.SKUID == 0 || command.Quantity <= 0 {
		return ErrInvalidRequest
	}
	return nil
}

func validOpaqueID(value string) bool {
	if strings.TrimSpace(value) != value || value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func sameReservation(reservation Reservation, command ReserveCommand) bool {
	return reservation.OrderID == command.OrderID && reservation.UserID == command.UserID && reservation.SKUID == command.SKUID && reservation.Quantity == command.Quantity && reservation.Mode == command.Mode
}

// AvailableStock 返回测试当前可用库存，不作为跨服务生产接口。
func (s *MemoryStore) AvailableStock(skuID uint64) int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stock[skuID]
}

var _ Store = (*MemoryStore)(nil)
