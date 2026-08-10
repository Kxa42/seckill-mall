package fulfillmentservice

import (
	"context"
	"fmt"
	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/messaging"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu        sync.RWMutex
	nextID    uint64
	shipments map[string]Shipment
	tracking  map[string]string
	eventSink messaging.EventSink
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{nextID: 1, shipments: make(map[string]Shipment), tracking: make(map[string]string)}
}

// SetEventSink 注入 Fulfillment 自有 Outbox。
func (r *MemoryRepository) SetEventSink(sink messaging.EventSink) { r.eventSink = sink }

func trackingKey(carrier, number string) string { return carrier + ":" + number }

func (r *MemoryRepository) Create(_ context.Context, value Shipment, _ time.Time) (Shipment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.shipments[value.OrderID]; ok {
		if existing.Carrier != value.Carrier || existing.TrackingNo != value.TrackingNo {
			return Shipment{}, false, ErrConflict
		}
		return existing, true, nil
	}
	key := trackingKey(value.Carrier, value.TrackingNo)
	if owner, exists := r.tracking[key]; exists && owner != value.OrderID {
		return Shipment{}, false, ErrConflict
	}
	if r.eventSink != nil {
		event, err := shipmentEvent(fmt.Sprintf("shipment.created:%s", value.OrderID), contracts.EventShipmentCreated, value, value.CreatedAt)
		if err != nil {
			return Shipment{}, false, err
		}
		if err := r.eventSink.AppendEvent(context.Background(), event, nil); err != nil {
			return Shipment{}, false, err
		}
	}
	value.ID = r.nextID
	r.nextID++
	r.shipments[value.OrderID], r.tracking[key] = value, value.OrderID
	return value, false, nil
}

func (r *MemoryRepository) Get(_ context.Context, orderID string) (Shipment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.shipments[orderID]
	if !ok {
		return Shipment{}, ErrNotFound
	}
	return value, nil
}

func (r *MemoryRepository) MarkReceived(_ context.Context, orderID string, now time.Time) (Shipment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.shipments[orderID]
	if !ok {
		return Shipment{}, false, ErrNotFound
	}
	if value.Status == StatusReceived {
		return value, true, nil
	}
	value.Status, value.DeliveredAt, value.UpdatedAt = StatusReceived, now, now
	if r.eventSink != nil {
		event, err := shipmentEvent(fmt.Sprintf("shipment.delivered:%s", value.OrderID), contracts.EventShipmentDelivered, value, now)
		if err != nil {
			return Shipment{}, false, err
		}
		if err := r.eventSink.AppendEvent(context.Background(), event, nil); err != nil {
			return Shipment{}, false, err
		}
	}
	r.shipments[orderID] = value
	return value, false, nil
}

var _ Repository = (*MemoryRepository)(nil)
