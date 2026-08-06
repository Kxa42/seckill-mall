// Package inventoryservice 定义 Inventory/Seckill Service 的库存状态模型。
// 普通库存和秒杀准入共享 reservation 状态机，但数据实现可分别使用内存或 Redis。
package inventoryservice

import "time"

const (
	ReservationReserved  = "reserved"
	ReservationConfirmed = "confirmed"
	ReservationReleased  = "released"
	ReservationRestocked = "restocked"
	ReservationExpired   = "expired"
)

// Reservation 是库存服务内部保存的预占记录。
type Reservation struct {
	ReservationID string
	OrderID       string
	UserID        uint64
	ActivityID    uint64
	SKUID         uint64
	Quantity      int32
	Status        string
	Mode          string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ReserveCommand 描述普通订单库存预占。
type ReserveCommand struct {
	ReservationID string
	OrderID       string
	UserID        uint64
	SKUID         uint64
	Quantity      int32
	Mode          string
}

// SeckillAdmissionCommand 描述秒杀热点准入。
type SeckillAdmissionCommand struct {
	RequestID  string
	ActivityID uint64
	UserID     uint64
	SKUID      uint64
	Quantity   int32
	OrderID    string
}
