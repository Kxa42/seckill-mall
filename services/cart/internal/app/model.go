// Package cartservice 定义购物车领域模型和应用服务。
// 购物车只保存用户与 SKU 数量，商品名称和价格由 Catalog 服务实时提供。
package cartservice

type SKU struct {
	ID             uint64
	Code, Name     string
	PriceCents     int64
	Active         bool
	AvailableStock int32
}

type Item struct {
	UserID, SKUID uint64
	Quantity      int32
	SKU           SKU
	SubtotalCents int64
}

type Preview struct {
	Items            []Item
	TotalAmountCents int64
	Available        bool
}
