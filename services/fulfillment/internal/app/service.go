package fulfillmentservice

import (
	"context"
	"errors"
	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/messaging"
	"strings"
	"time"
)

type Service struct {
	repository Repository
	orders     OrderClient
	now        func() time.Time
}

func NewService(repository Repository, orders OrderClient) (*Service, error) {
	if repository == nil || orders == nil {
		return nil, errors.New("fulfillment repository and order client are required")
	}
	return &Service{repository: repository, orders: orders, now: time.Now}, nil
}

func (s *Service) Ship(ctx context.Context, actorID uint64, orderID, carrier, trackingNo string) (Shipment, OrderTransition, bool, error) {
	orderID, carrier, trackingNo = strings.TrimSpace(orderID), strings.TrimSpace(carrier), strings.TrimSpace(trackingNo)
	if actorID == 0 || orderID == "" || carrier == "" || trackingNo == "" || len(carrier) > 64 || len(trackingNo) > 128 {
		return Shipment{}, OrderTransition{}, false, ErrInvalidRequest
	}
	if existing, err := s.repository.Get(ctx, orderID); err == nil {
		if existing.Carrier != carrier || existing.TrackingNo != trackingNo {
			return Shipment{}, OrderTransition{}, false, ErrConflict
		}
		transition, callErr := s.orders.Ship(ctx, actorID, orderID, carrier, trackingNo)
		return existing, transition, true, callErr
	} else if !errors.Is(err, ErrNotFound) {
		return Shipment{}, OrderTransition{}, false, err
	}
	now := s.now().UTC()
	value, reused, err := s.repository.Create(ctx, Shipment{OrderID: orderID, Carrier: carrier, TrackingNo: trackingNo, Status: StatusShipped, ShippedAt: now, CreatedAt: now, UpdatedAt: now}, now)
	if err != nil {
		return Shipment{}, OrderTransition{}, false, err
	}
	transition, err := s.orders.Ship(ctx, actorID, orderID, carrier, trackingNo)
	if err != nil {
		return Shipment{}, OrderTransition{}, false, err
	}
	return value, transition, reused, nil
}

func (s *Service) ConfirmReceipt(ctx context.Context, userID uint64, orderID string) (Shipment, OrderTransition, bool, error) {
	orderID = strings.TrimSpace(orderID)
	if userID == 0 || orderID == "" {
		return Shipment{}, OrderTransition{}, false, ErrInvalidRequest
	}
	if err := s.orders.Get(ctx, userID, orderID); err != nil {
		return Shipment{}, OrderTransition{}, false, err
	}
	transition, err := s.orders.ConfirmReceipt(ctx, userID, orderID)
	if err != nil {
		return Shipment{}, OrderTransition{}, false, err
	}
	value, reused, err := s.repository.MarkReceived(ctx, orderID, s.now().UTC())
	return value, transition, reused, err
}

func (s *Service) Get(ctx context.Context, userID uint64, orderID string) (Shipment, error) {
	orderID = strings.TrimSpace(orderID)
	if userID == 0 || orderID == "" {
		return Shipment{}, ErrInvalidRequest
	}
	if err := s.orders.Get(ctx, userID, orderID); err != nil {
		return Shipment{}, err
	}
	return s.repository.Get(ctx, orderID)
}

func shipmentEvent(eventID, eventType string, value Shipment, now time.Time) (contracts.EventEnvelope, error) {
	return messaging.NewEvent(eventID, eventType, "order", value.OrderID, contracts.ShipmentPayload{OrderID: value.OrderID, Carrier: value.Carrier, TrackingNo: value.TrackingNo, Status: value.Status}, now)
}
