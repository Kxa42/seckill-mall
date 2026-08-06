package order

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/common/internalcall"
	"seckill-mall/common/pb"
)

type CatalogClient interface {
	GetSKUSnapshot(ctx context.Context, skuIDs []uint64) ([]SKU, error)
}

type IdentityClient interface {
	GetAddressSnapshot(ctx context.Context, userID, addressID uint64) (Address, error)
}

type InventoryClient interface {
	Reserve(ctx context.Context, command ReserveCommand) (Reservation, error)
	AdmitSeckill(ctx context.Context, command ReserveCommand) (Reservation, error)
	Confirm(ctx context.Context, reservationID, orderID string) error
	Release(ctx context.Context, reservationID, orderID string) error
	Restock(ctx context.Context, reservationID, orderID string) error
}

type GRPCCatalogClient struct{ client pb.CatalogServiceClient }

func NewGRPCCatalogClient(client pb.CatalogServiceClient) *GRPCCatalogClient {
	return &GRPCCatalogClient{client: client}
}

func (c *GRPCCatalogClient) GetSKUSnapshot(ctx context.Context, skuIDs []uint64) ([]SKU, error) {
	if c == nil || c.client == nil {
		return nil, NewError(CodeUnavailable, "目录服务客户端未配置", nil)
	}
	response, err := c.client.GetSKUSnapshot(ctx, &pb.CatalogGetSKUSnapshotRequest{SkuIds: skuIDs})
	if err != nil {
		return nil, mapDependencyError(err, "目录服务暂时不可用")
	}
	items := make([]SKU, 0, len(response.GetItems()))
	for _, item := range response.GetItems() {
		if item == nil {
			return nil, NewError(CodeUnavailable, "目录服务返回了无效 SKU", nil)
		}
		items = append(items, SKU{ID: item.GetSkuId(), Code: item.GetSkuCode(), Name: item.GetSkuName(), PriceCents: item.GetPriceCents(), Active: item.GetActive()})
	}
	return items, nil
}

type GRPCIdentityClient struct{ client pb.IdentityServiceClient }

func NewGRPCIdentityClient(client pb.IdentityServiceClient) *GRPCIdentityClient {
	return &GRPCIdentityClient{client: client}
}

func (c *GRPCIdentityClient) GetAddressSnapshot(ctx context.Context, userID, addressID uint64) (Address, error) {
	if c == nil || c.client == nil {
		return Address{}, NewError(CodeUnavailable, "身份服务客户端未配置", nil)
	}
	ctx = internalcall.AppendUser(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), pb.IdentityService_GetAddressSnapshot_FullMethodName, userID, time.Now())
	value, err := c.client.GetAddressSnapshot(ctx, &pb.IdentityAddressSnapshotRequest{UserId: userID, AddressId: addressID})
	if err != nil {
		return Address{}, mapDependencyError(err, "身份服务暂时不可用")
	}
	return Address{ID: value.GetAddressId(), UserID: value.GetUserId(), Recipient: value.GetRecipient(), Phone: value.GetPhone(), Province: value.GetProvince(), City: value.GetCity(), District: value.GetDistrict(), Detail: value.GetDetail()}, nil
}

type GRPCInventoryClient struct{ client pb.InventoryServiceClient }

func NewGRPCInventoryClient(client pb.InventoryServiceClient) *GRPCInventoryClient {
	return &GRPCInventoryClient{client: client}
}

func (c *GRPCInventoryClient) Reserve(ctx context.Context, command ReserveCommand) (Reservation, error) {
	if c == nil || c.client == nil {
		return Reservation{}, NewError(CodeUnavailable, "库存服务客户端未配置", nil)
	}
	response, err := c.client.Reserve(ctx, &pb.InventoryReserveRequest{ReservationId: command.ReservationID, OrderId: command.OrderID, UserId: command.UserID, SkuId: command.SKUID, Quantity: command.Quantity, Mode: command.Mode})
	if err != nil {
		return Reservation{}, mapDependencyError(err, "库存服务暂时不可用")
	}
	return Reservation{ReservationID: response.GetReservationId(), OrderID: command.OrderID, SKUID: command.SKUID, Quantity: command.Quantity, Status: response.GetStatus()}, nil
}

func (c *GRPCInventoryClient) AdmitSeckill(ctx context.Context, command ReserveCommand) (Reservation, error) {
	if c == nil || c.client == nil {
		return Reservation{}, NewError(CodeUnavailable, "库存服务客户端未配置", nil)
	}
	response, err := c.client.AdmitSeckill(ctx, &pb.InventorySeckillAdmitRequest{RequestId: command.ReservationID, ActivityId: command.ActivityID, UserId: command.UserID, SkuId: command.SKUID, Quantity: command.Quantity, OrderId: command.OrderID})
	if err != nil {
		return Reservation{}, mapDependencyError(err, "库存服务暂时不可用")
	}
	return Reservation{ReservationID: response.GetAdmissionId(), OrderID: command.OrderID, SKUID: command.SKUID, Quantity: command.Quantity, Status: response.GetStatus()}, nil
}

func (c *GRPCInventoryClient) Confirm(ctx context.Context, reservationID, orderID string) error {
	if c == nil || c.client == nil {
		return NewError(CodeUnavailable, "库存服务客户端未配置", nil)
	}
	_, err := c.client.Confirm(ctx, &pb.InventoryReservationRequest{ReservationId: reservationID, OrderId: orderID})
	if err != nil {
		return mapDependencyError(err, "库存服务暂时不可用")
	}
	return nil
}

func (c *GRPCInventoryClient) Release(ctx context.Context, reservationID, orderID string) error {
	if c == nil || c.client == nil {
		return NewError(CodeUnavailable, "库存服务客户端未配置", nil)
	}
	_, err := c.client.Release(ctx, &pb.InventoryReservationRequest{ReservationId: reservationID, OrderId: orderID})
	if err != nil {
		return mapDependencyError(err, "库存服务暂时不可用")
	}
	return nil
}

func (c *GRPCInventoryClient) Restock(ctx context.Context, reservationID, orderID string) error {
	if c == nil || c.client == nil {
		return NewError(CodeUnavailable, "库存服务客户端未配置", nil)
	}
	_, err := c.client.Restock(ctx, &pb.InventoryReservationRequest{ReservationId: reservationID, OrderId: orderID})
	if err != nil {
		return mapDependencyError(err, "库存服务暂时不可用")
	}
	return nil
}

func mapDependencyError(err error, fallback string) error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		return err
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return NewError(CodeValidation, "下游请求参数无效", err)
	case codes.NotFound:
		return NewError(CodeNotFound, "下游资源不存在", err)
	case codes.ResourceExhausted:
		return NewError(CodeOutOfStock, "库存不足", err)
	case codes.AlreadyExists, codes.Aborted:
		return NewError(CodeConflict, "下游操作冲突", err)
	default:
		return NewError(CodeUnavailable, fallback, fmt.Errorf("%w", err))
	}
}
