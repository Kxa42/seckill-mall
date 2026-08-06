// Package contracts 定义商城微服务的边界、数据所有权和跨服务依赖。
// 这些描述只表达稳定的领域契约，不允许引入数据库、HTTP 或消息客户端实现。
package contracts

import "fmt"

// ServiceBoundary 描述一个业务服务拥有的数据，以及允许使用的协作方式。
// OwnedData 使用逻辑数据集名称，而不是具体数据库表名，便于迁移期间调整表前缀。
type ServiceBoundary struct {
	Service                 string
	OwnedData               []string
	SynchronousDependencies []string
	PublishedEvents         []string
	ConsumedEvents          []string
}

var serviceBoundaryCatalog = []ServiceBoundary{
	{
		Service:   ServiceIdentity,
		OwnedData: []string{"users", "refresh_tokens", "user_addresses"},
	},
	{
		Service:         ServiceCatalog,
		OwnedData:       []string{"categories", "spus", "skus", "product_images"},
		PublishedEvents: []string{},
		ConsumedEvents:  []string{},
	},
	{
		Service:         ServiceInventory,
		OwnedData:       []string{"inventory_stock", "inventory_reservations", "seckill_activities", "seckill_user_limits"},
		PublishedEvents: []string{EventSeckillAccepted, EventInventoryReserved, EventInventoryReleased, EventInventoryRestocked},
	},
	{
		Service:   ServiceCart,
		OwnedData: []string{"cart_items"},
	},
	{
		Service:                 ServiceOrder,
		OwnedData:               []string{"commerce_orders", "order_items", "order_status_history", "order_create_intents", "order_operations", "order_outbox_events", "order_inbox_events"},
		SynchronousDependencies: []string{ServiceCatalog, ServiceIdentity, ServiceInventory},
		PublishedEvents:         []string{EventOrderCreated, EventOrderCancelled},
		ConsumedEvents:          []string{EventPaymentSucceeded, EventPaymentRefunded, EventInventoryReserved, EventInventoryReleased, EventShipmentCreated, EventShipmentDelivered},
	},
	{
		Service:         ServicePayment,
		OwnedData:       []string{"payments", "refunds", "payment_outbox_events", "payment_inbox_events"},
		PublishedEvents: []string{EventPaymentSucceeded, EventPaymentRefunded},
		ConsumedEvents:  []string{EventOrderCreated, EventOrderCancelled},
	},
	{
		Service:         ServiceFulfillment,
		OwnedData:       []string{"shipments", "fulfillment_outbox_events", "fulfillment_inbox_events"},
		PublishedEvents: []string{EventShipmentCreated, EventShipmentDelivered},
		ConsumedEvents:  []string{EventPaymentSucceeded},
	},
}

// ServiceBoundaries 返回服务边界的副本，调用方修改结果不会影响全局契约。
func ServiceBoundaries() []ServiceBoundary {
	boundaries := make([]ServiceBoundary, len(serviceBoundaryCatalog))
	for i, boundary := range serviceBoundaryCatalog {
		boundaries[i] = cloneBoundary(boundary)
	}
	return boundaries
}

// ServiceBoundaryFor 返回指定业务服务的边界定义。
func ServiceBoundaryFor(service string) (ServiceBoundary, bool) {
	for _, boundary := range serviceBoundaryCatalog {
		if boundary.Service == service {
			return cloneBoundary(boundary), true
		}
	}
	return ServiceBoundary{}, false
}

// ValidateServiceBoundaries 检查所有权没有重叠，且边界引用的服务和事件均有效。
func ValidateServiceBoundaries() error {
	ownedBy := make(map[string]string)
	seenServices := make(map[string]struct{}, len(serviceBoundaryCatalog))
	for _, boundary := range serviceBoundaryCatalog {
		if boundary.Service == "" {
			return fmt.Errorf("service boundary service is required")
		}
		if _, exists := seenServices[boundary.Service]; exists {
			return fmt.Errorf("duplicate service boundary: %s", boundary.Service)
		}
		seenServices[boundary.Service] = struct{}{}
		for _, dataSet := range boundary.OwnedData {
			if previous, exists := ownedBy[dataSet]; exists {
				return fmt.Errorf("data set %q owned by both %s and %s", dataSet, previous, boundary.Service)
			}
			ownedBy[dataSet] = boundary.Service
		}
		for _, dependency := range boundary.SynchronousDependencies {
			if !boundaryCatalogContains(dependency) {
				return fmt.Errorf("service %s depends on unknown service %s", boundary.Service, dependency)
			}
		}
		for _, eventType := range append(append([]string{}, boundary.PublishedEvents...), boundary.ConsumedEvents...) {
			if !IsKnownEventType(eventType) {
				return fmt.Errorf("service %s references unknown event %s", boundary.Service, eventType)
			}
		}
	}
	return nil
}

func boundaryCatalogContains(service string) bool {
	for _, boundary := range serviceBoundaryCatalog {
		if boundary.Service == service {
			return true
		}
	}
	return false
}

func cloneBoundary(boundary ServiceBoundary) ServiceBoundary {
	boundary.OwnedData = append([]string(nil), boundary.OwnedData...)
	boundary.SynchronousDependencies = append([]string(nil), boundary.SynchronousDependencies...)
	boundary.PublishedEvents = append([]string(nil), boundary.PublishedEvents...)
	boundary.ConsumedEvents = append([]string(nil), boundary.ConsumedEvents...)
	return boundary
}
