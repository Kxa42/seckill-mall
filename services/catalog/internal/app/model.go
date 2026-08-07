// Package catalogservice 定义 Catalog Service 的独立领域模型。
// 该包不依赖 internal/commerce，避免目录服务反向访问商城总 Repository。
package catalogservice

import "time"

// SKU 是目录服务对外提供的商品单元快照。
type SKU struct {
	ID             uint64
	SPUID          uint64
	Code           string
	Name           string
	PriceCents     int64
	AvailableStock int32
	ReservedStock  int32
	Active         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Product 是 Catalog Service 管理的商品聚合只读视图。
type Product struct {
	SPUID       uint64
	CategoryID  uint64
	Name        string
	Description string
	Active      bool
	SKUs        []SKU
	Images      []string
}

// ProductPage 是商品分页结果。
type ProductPage struct {
	Items  []Product
	Total  int64
	Offset int
	Limit  int
}
