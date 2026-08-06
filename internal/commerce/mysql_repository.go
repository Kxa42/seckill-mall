package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MySQLRepository 使用 GORM 实现商城领域的事务与持久化。
type MySQLRepository struct {
	db *gorm.DB
}

// NewMySQLRepository 创建 MySQL Repository。
func NewMySQLRepository(db *gorm.DB) (*MySQLRepository, error) {
	if db == nil {
		return nil, errors.New("db 不能为空")
	}
	return &MySQLRepository{db: db}, nil
}

type userRecord struct {
	ID           uint64 `gorm:"column:id;primaryKey"`
	Email        string
	PasswordHash string
	Role         string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (userRecord) TableName() string { return "users" }

type refreshTokenRecord struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	UserID    uint64
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

func (refreshTokenRecord) TableName() string { return "refresh_tokens" }

type addressRecord struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	UserID    uint64
	Recipient string
	Phone     string
	Province  string
	City      string
	District  string
	Detail    string
	IsDefault bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (addressRecord) TableName() string { return "user_addresses" }

type categoryRecord struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	ParentID  uint64
	Name      string
	Slug      string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (categoryRecord) TableName() string { return "categories" }

type spuRecord struct {
	ID          uint64 `gorm:"column:id;primaryKey"`
	CategoryID  uint64
	Name        string
	Description string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (spuRecord) TableName() string { return "spus" }

type skuRecord struct {
	ID             uint64 `gorm:"column:id;primaryKey"`
	SPUID          uint64 `gorm:"column:spu_id"`
	Code           string
	Name           string
	PriceCents     int64
	AvailableStock int32
	ReservedStock  int32
	Active         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (skuRecord) TableName() string { return "skus" }

type productImageRecord struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	SPUID     uint64 `gorm:"column:spu_id"`
	URL       string
	SortOrder int
	CreatedAt time.Time
}

func (productImageRecord) TableName() string { return "product_images" }

type cartRecord struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	UserID    uint64
	SKUID     uint64 `gorm:"column:sku_id"`
	Quantity  int32
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (cartRecord) TableName() string { return "cart_items" }

type orderRecord struct {
	ID               uint64 `gorm:"column:id;primaryKey"`
	OrderID          string
	UserID           uint64
	OrderType        string
	Status           string
	TotalAmountCents int64
	IdempotencyKey   string
	AddressSnapshot  string `gorm:"type:json"`
	ExpiresAt        time.Time
	PaidAt           *time.Time
	ShippedAt        *time.Time
	CompletedAt      *time.Time
	CanceledAt       *time.Time
	RefundedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (orderRecord) TableName() string { return "commerce_orders" }

type orderItemRecord struct {
	ID             uint64 `gorm:"column:id;primaryKey"`
	OrderID        string
	SKUID          uint64 `gorm:"column:sku_id"`
	SKUCode        string `gorm:"column:sku_code"`
	SKUName        string `gorm:"column:sku_name"`
	UnitPriceCents int64
	Quantity       int32
	SubtotalCents  int64
	CreatedAt      time.Time
}

func (orderItemRecord) TableName() string { return "order_items" }

type reservationRecord struct {
	ID            uint64 `gorm:"column:id;primaryKey"`
	ReservationID string
	OrderID       string
	SKUID         uint64 `gorm:"column:sku_id"`
	Quantity      int32
	Status        string
	ExpiresAt     time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (reservationRecord) TableName() string { return "inventory_reservations" }

type statusRecord struct {
	ID         uint64 `gorm:"column:id;primaryKey"`
	OrderID    string
	FromStatus string
	ToStatus   string
	Reason     string
	ActorType  string
	ActorID    uint64
	CreatedAt  time.Time
}

func (statusRecord) TableName() string { return "order_status_history" }

type paymentRecord struct {
	ID          uint64 `gorm:"column:id;primaryKey"`
	PaymentNo   string
	OrderID     string
	AmountCents int64
	Provider    string
	Status      string
	CallbackRef *string
	PaidAt      *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (paymentRecord) TableName() string { return "payments" }

type refundRecord struct {
	ID          uint64 `gorm:"column:id;primaryKey"`
	RefundNo    string
	OrderID     string
	PaymentNo   string
	AmountCents int64
	Reason      string
	Status      string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

func (refundRecord) TableName() string { return "refunds" }

type shipmentRecord struct {
	ID          uint64 `gorm:"column:id;primaryKey"`
	OrderID     string
	Carrier     string
	TrackingNo  string
	Status      string
	ShippedAt   time.Time
	DeliveredAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (shipmentRecord) TableName() string { return "shipments" }

type outboxRecord struct {
	ID            uint64 `gorm:"column:id;primaryKey"`
	EventID       string
	AggregateType string
	AggregateID   string
	EventType     string
	EventVersion  int
	Payload       string  `gorm:"type:json"`
	Headers       *string `gorm:"type:json"`
	Status        string
	RetryCount    int
	NextRetryAt   time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (outboxRecord) TableName() string { return "commerce_outbox_events" }

func (r *MySQLRepository) CreateUser(ctx context.Context, email, passwordHash, role string, now time.Time) (User, error) {
	record := userRecord{Email: email, PasswordHash: passwordHash, Role: role, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := r.db.WithContext(ctx).Create(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return User{}, NewError(CodeConflict, "邮箱已注册", err)
		}
		return User{}, fmt.Errorf("创建用户: %w", err)
	}
	return userFromRecord(record), nil
}

func (r *MySQLRepository) FindUserByEmail(ctx context.Context, email string) (User, error) {
	var record userRecord
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&record).Error; err != nil {
		return User{}, mapNotFound(err, "用户不存在")
	}
	return userFromRecord(record), nil
}

func (r *MySQLRepository) FindUserByID(ctx context.Context, userID uint64) (User, error) {
	var record userRecord
	if err := r.db.WithContext(ctx).First(&record, userID).Error; err != nil {
		return User{}, mapNotFound(err, "用户不存在")
	}
	return userFromRecord(record), nil
}

func (r *MySQLRepository) StoreRefreshToken(ctx context.Context, token RefreshTokenRecord) error {
	record := refreshTokenRecord{
		UserID: token.UserID, TokenHash: token.TokenHash, ExpiresAt: token.ExpiresAt,
		RevokedAt: token.RevokedAt, CreatedAt: token.CreatedAt,
	}
	if err := r.db.WithContext(ctx).Create(&record).Error; err != nil {
		return fmt.Errorf("保存刷新令牌: %w", err)
	}
	return nil
}

func (r *MySQLRepository) RotateRefreshToken(ctx context.Context, oldHash string, replacement RefreshTokenRecord, now time.Time) (User, error) {
	var user User
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current refreshTokenRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ?", oldHash).First(&current).Error; err != nil {
			return NewError(CodeUnauthorized, "刷新令牌无效或已过期", err)
		}
		if current.RevokedAt != nil || !current.ExpiresAt.After(now) {
			return NewError(CodeUnauthorized, "刷新令牌无效或已过期", nil)
		}
		if err := tx.Model(&current).Update("revoked_at", now).Error; err != nil {
			return err
		}
		replacementRow := refreshTokenRecord{
			UserID: current.UserID, TokenHash: replacement.TokenHash,
			ExpiresAt: replacement.ExpiresAt, CreatedAt: replacement.CreatedAt,
		}
		if err := tx.Create(&replacementRow).Error; err != nil {
			return err
		}
		var record userRecord
		if err := tx.Where("id = ? AND status = ?", current.UserID, UserStatusActive).First(&record).Error; err != nil {
			return NewError(CodeUnauthorized, "用户不可用", err)
		}
		user = userFromRecord(record)
		return nil
	})
	return user, err
}

func (r *MySQLRepository) EnsureAdmin(ctx context.Context, email, passwordHash string, now time.Time) error {
	record := userRecord{Email: email, PasswordHash: passwordHash, Role: RoleAdmin, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "email"}},
		DoUpdates: clause.Assignments(map[string]any{
			"password_hash": passwordHash, "role": RoleAdmin, "status": UserStatusActive, "updated_at": now,
		}),
	}).Create(&record).Error
}

func (r *MySQLRepository) CreateAddress(ctx context.Context, address Address, now time.Time) (Address, error) {
	var result Address
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if address.IsDefault {
			if err := tx.Model(&addressRecord{}).Where("user_id = ?", address.UserID).Updates(map[string]any{"is_default": false, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		record := addressToRecord(address)
		record.CreatedAt = now
		record.UpdatedAt = now
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		result = addressFromRecord(record)
		return nil
	})
	if err != nil {
		return Address{}, fmt.Errorf("创建地址: %w", err)
	}
	return result, nil
}

func (r *MySQLRepository) UpdateAddress(ctx context.Context, address Address, now time.Time) (Address, error) {
	var result Address
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current addressRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", address.ID, address.UserID).First(&current).Error; err != nil {
			return mapNotFound(err, "地址不存在")
		}
		if address.IsDefault {
			if err := tx.Model(&addressRecord{}).Where("user_id = ? AND id <> ?", address.UserID, address.ID).Updates(map[string]any{"is_default": false, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		record := addressToRecord(address)
		record.CreatedAt = current.CreatedAt
		record.UpdatedAt = now
		if err := tx.Model(&current).Select("recipient", "phone", "province", "city", "district", "detail", "is_default", "updated_at").Updates(record).Error; err != nil {
			return err
		}
		result = addressFromRecord(record)
		return nil
	})
	return result, err
}

func (r *MySQLRepository) DeleteAddress(ctx context.Context, userID, addressID uint64) error {
	result := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", addressID, userID).Delete(&addressRecord{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return NewError(CodeNotFound, "地址不存在", nil)
	}
	return nil
}

func (r *MySQLRepository) ListAddresses(ctx context.Context, userID uint64) ([]Address, error) {
	var records []addressRecord
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("is_default DESC, id ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	addresses := make([]Address, 0, len(records))
	for _, record := range records {
		addresses = append(addresses, addressFromRecord(record))
	}
	return addresses, nil
}

func (r *MySQLRepository) ListProducts(ctx context.Context, offset, limit int) (ProductPage, error) {
	var total int64
	db := r.db.WithContext(ctx).Model(&spuRecord{}).Where("active = ?", true)
	if err := db.Count(&total).Error; err != nil {
		return ProductPage{}, err
	}
	var records []spuRecord
	if err := db.Order("id ASC").Offset(offset).Limit(limit).Find(&records).Error; err != nil {
		return ProductPage{}, err
	}
	products, err := r.productsFromSPUs(ctx, records, false)
	if err != nil {
		return ProductPage{}, err
	}
	return ProductPage{Items: products, Offset: offset, Limit: limit, Total: total}, nil
}

func (r *MySQLRepository) GetProduct(ctx context.Context, productID uint64) (Product, error) {
	var record spuRecord
	if err := r.db.WithContext(ctx).Where("id = ? AND active = ?", productID, true).First(&record).Error; err != nil {
		return Product{}, mapNotFound(err, "商品不存在或未上架")
	}
	products, err := r.productsFromSPUs(ctx, []spuRecord{record}, false)
	if err != nil {
		return Product{}, err
	}
	return products[0], nil
}

func (r *MySQLRepository) UpsertProduct(ctx context.Context, input AdminProductInput, now time.Time) (Product, error) {
	var productID uint64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		category := categoryRecord{
			ID: input.Category.ID, ParentID: input.Category.ParentID, Name: input.Category.Name,
			Slug: input.Category.Slug, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		if category.ID == 0 {
			if err := tx.Create(&category).Error; err != nil {
				return err
			}
		} else if err := tx.Save(&category).Error; err != nil {
			return err
		}
		spu := spuRecord{
			ID: input.SPU.ID, CategoryID: category.ID, Name: input.SPU.Name,
			Description: input.SPU.Description, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		if spu.ID == 0 {
			if err := tx.Create(&spu).Error; err != nil {
				return err
			}
		} else if err := tx.Save(&spu).Error; err != nil {
			return err
		}
		productID = spu.ID
		for _, sku := range input.SKUs {
			record := skuRecord{
				ID: sku.ID, SPUID: spu.ID, Code: sku.Code, Name: sku.Name,
				PriceCents: sku.PriceCents, AvailableStock: sku.AvailableStock,
				Active: true, CreatedAt: now, UpdatedAt: now,
			}
			if record.ID == 0 {
				if err := tx.Create(&record).Error; err != nil {
					return err
				}
			} else {
				var existing skuRecord
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, record.ID).Error; err != nil {
					return mapNotFound(err, "SKU 不存在")
				}
				record.ReservedStock = existing.ReservedStock
				record.CreatedAt = existing.CreatedAt
				if err := tx.Model(&existing).Select(
					"spu_id", "code", "name", "price_cents", "available_stock", "active", "updated_at",
				).Updates(record).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return Product{}, NewError(CodeConflict, "分类 slug 或 SKU code 已存在", err)
		}
		return Product{}, err
	}
	return r.getProductIncludingInactive(ctx, productID)
}

func (r *MySQLRepository) SetCartItem(ctx context.Context, userID, skuID uint64, quantity int32, now time.Time) (CartItem, error) {
	var sku skuRecord
	if err := r.db.WithContext(ctx).Where("id = ? AND active = ?", skuID, true).First(&sku).Error; err != nil {
		return CartItem{}, mapNotFound(err, "SKU 不存在或未上架")
	}
	record := cartRecord{UserID: userID, SKUID: skuID, Quantity: quantity, CreatedAt: now, UpdatedAt: now}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "sku_id"}},
		DoUpdates: clause.Assignments(map[string]any{"quantity": quantity, "updated_at": now}),
	}).Create(&record).Error; err != nil {
		return CartItem{}, err
	}
	if err := r.db.WithContext(ctx).Where("user_id = ? AND sku_id = ?", userID, skuID).First(&record).Error; err != nil {
		return CartItem{}, err
	}
	return cartFromRecords(record, sku), nil
}

func (r *MySQLRepository) DeleteCartItem(ctx context.Context, userID, skuID uint64) error {
	result := r.db.WithContext(ctx).Where("user_id = ? AND sku_id = ?", userID, skuID).Delete(&cartRecord{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return NewError(CodeNotFound, "购物车商品不存在", nil)
	}
	return nil
}

func (r *MySQLRepository) ListCartItems(ctx context.Context, userID uint64) ([]CartItem, error) {
	var carts []cartRecord
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("id ASC").Find(&carts).Error; err != nil {
		return nil, err
	}
	if len(carts) == 0 {
		return []CartItem{}, nil
	}
	ids := make([]uint64, 0, len(carts))
	for _, cart := range carts {
		ids = append(ids, cart.SKUID)
	}
	var skus []skuRecord
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&skus).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint64]skuRecord, len(skus))
	for _, sku := range skus {
		byID[sku.ID] = sku
	}
	items := make([]CartItem, 0, len(carts))
	for _, cart := range carts {
		items = append(items, cartFromRecords(cart, byID[cart.SKUID]))
	}
	return items, nil
}

func (r *MySQLRepository) CreateOrder(ctx context.Context, command CreateOrderCommand) (Order, bool, error) {
	var created Order
	var reused bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing orderRecord
		err := tx.Where("user_id = ? AND idempotency_key = ?", command.UserID, command.IdempotencyKey).First(&existing).Error
		if err == nil {
			reused = true
			var loadErr error
			created, loadErr = r.loadOrder(tx, command.UserID, existing.OrderID)
			return loadErr
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var address addressRecord
		if err := tx.Where("id = ? AND user_id = ?", command.AddressID, command.UserID).First(&address).Error; err != nil {
			return mapNotFound(err, "收货地址不存在")
		}
		requested, err := r.requestedItems(tx, command)
		if err != nil {
			return err
		}
		ids := make([]uint64, 0, len(requested))
		for skuID := range requested {
			ids = append(ids, skuID)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		var skus []skuRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id ASC").Find(&skus).Error; err != nil {
			return err
		}
		if len(skus) != len(ids) {
			return NewError(CodeNotFound, "部分 SKU 不存在", nil)
		}
		items := make([]orderItemRecord, 0, len(skus))
		reservations := make([]reservationRecord, 0, len(skus))
		var total int64
		for _, sku := range skus {
			quantity := requested[sku.ID]
			if !sku.Active {
				return NewError(CodeNotFound, "SKU 不存在或未上架", nil)
			}
			subtotal, err := CalculateSubtotal(sku.PriceCents, quantity)
			if err != nil {
				return err
			}
			result := tx.Model(&skuRecord{}).Where("id = ? AND active = ? AND available_stock >= ?", sku.ID, true, quantity).Updates(map[string]any{
				"available_stock": gorm.Expr("available_stock - ?", quantity),
				"reserved_stock":  gorm.Expr("reserved_stock + ?", quantity),
				"updated_at":      command.Now,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return NewError(CodeOutOfStock, "库存不足", nil)
			}
			total, err = addAmount(total, subtotal)
			if err != nil {
				return err
			}
			items = append(items, orderItemRecord{
				OrderID: command.OrderID, SKUID: sku.ID, SKUCode: sku.Code, SKUName: sku.Name,
				UnitPriceCents: sku.PriceCents, Quantity: quantity, SubtotalCents: subtotal, CreatedAt: command.Now,
			})
			reservations = append(reservations, reservationRecord{
				ReservationID: fmt.Sprintf("res_%s_%d", command.OrderID, sku.ID), OrderID: command.OrderID,
				SKUID: sku.ID, Quantity: quantity, Status: ReservationStatusReserved,
				ExpiresAt: command.ExpiresAt, CreatedAt: command.Now, UpdatedAt: command.Now,
			})
		}
		addressJSON, err := json.Marshal(addressFromRecord(address))
		if err != nil {
			return err
		}
		order := orderRecord{
			OrderID: command.OrderID, UserID: command.UserID, OrderType: command.OrderType,
			Status: OrderStatusPendingPayment, TotalAmountCents: total, IdempotencyKey: command.IdempotencyKey,
			AddressSnapshot: string(addressJSON), ExpiresAt: command.ExpiresAt, CreatedAt: command.Now, UpdatedAt: command.Now,
		}
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		if err := tx.Create(&reservations).Error; err != nil {
			return err
		}
		if err := tx.Create(&statusRecord{
			OrderID: command.OrderID, ToStatus: OrderStatusPendingPayment, Reason: "创建订单",
			ActorType: "user", ActorID: command.UserID, CreatedAt: command.Now,
		}).Error; err != nil {
			return err
		}
		if err := r.appendOutbox(tx, command.OrderID, "commerce.order.created.v1", command.Now, map[string]any{
			"order_id": command.OrderID, "user_id": command.UserID, "order_type": command.OrderType,
			"total_amount_cents": total,
		}); err != nil {
			return err
		}
		if command.OrderType == OrderTypeNormal {
			if err := tx.Where("user_id = ?", command.UserID).Delete(&cartRecord{}).Error; err != nil {
				return err
			}
		}
		var loadErr error
		created, loadErr = r.loadOrder(tx, command.UserID, command.OrderID)
		return loadErr
	})
	if err != nil && errors.Is(err, gorm.ErrDuplicatedKey) {
		var existing orderRecord
		if findErr := r.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", command.UserID, command.IdempotencyKey).First(&existing).Error; findErr == nil {
			order, loadErr := r.loadOrder(r.db.WithContext(ctx), command.UserID, existing.OrderID)
			return order, true, loadErr
		}
	}
	return created, reused, err
}

func (r *MySQLRepository) ListOrders(ctx context.Context, userID uint64, offset, limit int) ([]Order, int64, error) {
	var total int64
	db := r.db.WithContext(ctx).Model(&orderRecord{}).Where("user_id = ?", userID)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []orderRecord
	if err := db.Order("created_at DESC").Offset(offset).Limit(limit).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	orders := make([]Order, 0, len(records))
	for _, record := range records {
		order, err := r.loadOrder(r.db.WithContext(ctx), userID, record.OrderID)
		if err != nil {
			return nil, 0, err
		}
		orders = append(orders, order)
	}
	return orders, total, nil
}

func (r *MySQLRepository) GetOrder(ctx context.Context, userID uint64, orderID string) (Order, error) {
	return r.loadOrder(r.db.WithContext(ctx), userID, orderID)
}

func (r *MySQLRepository) CancelOrder(ctx context.Context, userID uint64, orderID, reason string, now time.Time) (Order, error) {
	var result Order
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record orderRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ? AND user_id = ?", orderID, userID).First(&record).Error; err != nil {
			return mapNotFound(err, "订单不存在")
		}
		if record.Status == OrderStatusCanceled {
			var err error
			result, err = r.loadOrder(tx, userID, orderID)
			return err
		}
		if !CanTransitionOrder(record.Status, OrderStatusCanceled) {
			return NewError(CodeInvalidTransition, "当前订单状态不能取消", nil)
		}
		if err := r.releaseReservations(tx, orderID, ReservationStatusReleased, now, false); err != nil {
			return err
		}
		if err := r.transition(tx, &record, OrderStatusCanceled, reason, "user", userID, now); err != nil {
			return err
		}
		record.CanceledAt = timePointer(now)
		if err := tx.Model(&record).Update("canceled_at", now).Error; err != nil {
			return err
		}
		if err := r.appendOutbox(tx, orderID, "commerce.order.canceled.v1", now, map[string]any{"order_id": orderID, "reason": reason}); err != nil {
			return err
		}
		var err error
		result, err = r.loadOrder(tx, userID, orderID)
		return err
	})
	return result, err
}

func (r *MySQLRepository) ExpireOrders(ctx context.Context, now time.Time, limit int) (int, error) {
	var records []orderRecord
	if err := r.db.WithContext(ctx).Where("status = ? AND expires_at <= ?", OrderStatusPendingPayment, now).Order("expires_at ASC").Limit(limit).Find(&records).Error; err != nil {
		return 0, err
	}
	count := 0
	for _, candidate := range records {
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var record orderRecord
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ?", candidate.OrderID).First(&record).Error; err != nil {
				return err
			}
			if record.Status != OrderStatusPendingPayment || record.ExpiresAt.After(now) {
				return nil
			}
			if err := r.releaseReservations(tx, record.OrderID, ReservationStatusExpired, now, false); err != nil {
				return err
			}
			if err := r.transition(tx, &record, OrderStatusCanceled, "支付超时", "system", 0, now); err != nil {
				return err
			}
			if err := tx.Model(&record).Update("canceled_at", now).Error; err != nil {
				return err
			}
			if err := r.appendOutbox(tx, record.OrderID, "commerce.order.expired.v1", now, map[string]any{"order_id": record.OrderID}); err != nil {
				return err
			}
			count++
			return nil
		})
		if err != nil {
			return count, err
		}
	}
	return count, nil
}

func (r *MySQLRepository) CreatePayment(ctx context.Context, userID uint64, orderID, paymentNo string, now time.Time) (Payment, error) {
	var result Payment
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order orderRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ? AND user_id = ?", orderID, userID).First(&order).Error; err != nil {
			return mapNotFound(err, "订单不存在")
		}
		var existing paymentRecord
		if err := tx.Where("order_id = ?", orderID).First(&existing).Error; err == nil {
			result = paymentFromRecord(existing)
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if order.Status != OrderStatusPendingPayment || !order.ExpiresAt.After(now) {
			return NewError(CodeInvalidTransition, "当前订单不能支付", nil)
		}
		record := paymentRecord{
			PaymentNo: paymentNo, OrderID: orderID, AmountCents: order.TotalAmountCents,
			Provider: "mock", Status: PaymentStatusPending, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		result = paymentFromRecord(record)
		return nil
	})
	return result, err
}

func (r *MySQLRepository) GetPayment(ctx context.Context, paymentNo string) (Payment, error) {
	var record paymentRecord
	if err := r.db.WithContext(ctx).Where("payment_no = ?", paymentNo).First(&record).Error; err != nil {
		return Payment{}, mapNotFound(err, "支付单不存在")
	}
	var order orderRecord
	if err := r.db.WithContext(ctx).Select("user_id").Where("order_id = ?", record.OrderID).First(&order).Error; err != nil {
		return Payment{}, mapNotFound(err, "订单不存在")
	}
	payment := paymentFromRecord(record)
	payment.UserID = order.UserID
	return payment, nil
}

func (r *MySQLRepository) MarkPaymentSucceeded(ctx context.Context, paymentNo, callbackRef string, now time.Time) (Payment, error) {
	var result Payment
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record paymentRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("payment_no = ?", paymentNo).First(&record).Error; err != nil {
			return mapNotFound(err, "支付单不存在")
		}
		if record.Status == PaymentStatusSucceeded {
			if record.CallbackRef == nil || *record.CallbackRef != callbackRef {
				return NewError(CodeConflict, "支付回调与已完成记录不一致", nil)
			}
			result = paymentFromRecord(record)
			return nil
		}
		record.Status = PaymentStatusSucceeded
		record.CallbackRef = &callbackRef
		record.PaidAt = timePointer(now)
		record.UpdatedAt = now
		if err := tx.Save(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return NewError(CodeConflict, "支付回调流水已使用", err)
			}
			return err
		}
		result = paymentFromRecord(record)
		return nil
	})
	return result, err
}

func (r *MySQLRepository) RecordShipment(ctx context.Context, orderID, carrier, trackingNo string, now time.Time) (Shipment, error) {
	var result Shipment
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order orderRecord
		if err := tx.Select("order_id").Where("order_id = ?", orderID).First(&order).Error; err != nil {
			return mapNotFound(err, "订单不存在")
		}
		var existing shipmentRecord
		if err := tx.Where("order_id = ?", orderID).First(&existing).Error; err == nil {
			if existing.Carrier != carrier || existing.TrackingNo != trackingNo {
				return NewError(CodeConflict, "订单已有不同物流记录", nil)
			}
			result = shipmentFromRecord(existing)
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		record := shipmentRecord{OrderID: orderID, Carrier: carrier, TrackingNo: trackingNo, Status: ShipmentStatusShipped, ShippedAt: now, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		result = shipmentFromRecord(record)
		return nil
	})
	return result, err
}

func (r *MySQLRepository) MarkShipmentReceived(ctx context.Context, orderID string, now time.Time) (Shipment, error) {
	var record shipmentRecord
	if err := r.db.WithContext(ctx).Where("order_id = ?", orderID).First(&record).Error; err != nil {
		return Shipment{}, mapNotFound(err, "物流记录不存在")
	}
	if err := r.db.WithContext(ctx).Model(&record).Updates(map[string]any{"status": ShipmentStatusReceived, "delivered_at": now, "updated_at": now}).Error; err != nil {
		return Shipment{}, err
	}
	record.Status = ShipmentStatusReceived
	record.DeliveredAt = timePointer(now)
	record.UpdatedAt = now
	return shipmentFromRecord(record), nil
}

func (r *MySQLRepository) RecordRefund(ctx context.Context, userID uint64, orderID, refundNo, reason string, now time.Time) (Refund, error) {
	var result Refund
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order orderRecord
		if err := tx.Where("order_id = ? AND user_id = ?", orderID, userID).First(&order).Error; err != nil {
			return mapNotFound(err, "订单不存在")
		}
		var existing refundRecord
		if err := tx.Where("order_id = ?", orderID).First(&existing).Error; err == nil {
			result = refundFromRecord(existing)
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var payment paymentRecord
		if err := tx.Where("order_id = ? AND status = ?", orderID, PaymentStatusSucceeded).First(&payment).Error; err != nil {
			return NewError(CodeInvalidTransition, "订单没有成功支付记录", err)
		}
		record := refundRecord{RefundNo: refundNo, OrderID: orderID, PaymentNo: payment.PaymentNo, AmountCents: order.TotalAmountCents, Reason: reason, Status: RefundStatusSucceeded, CreatedAt: now, CompletedAt: timePointer(now)}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		result = refundFromRecord(record)
		return nil
	})
	return result, err
}

func (r *MySQLRepository) CompletePayment(ctx context.Context, paymentNo, callbackRef string, now time.Time) (Order, error) {
	var result Order
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var payment paymentRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("payment_no = ?", paymentNo).First(&payment).Error; err != nil {
			return mapNotFound(err, "支付单不存在")
		}
		var order orderRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ?", payment.OrderID).First(&order).Error; err != nil {
			return err
		}
		if payment.Status == PaymentStatusSucceeded {
			if payment.CallbackRef == nil || *payment.CallbackRef != callbackRef {
				return NewError(CodeConflict, "支付回调与已完成记录不一致", nil)
			}
			var err error
			result, err = r.loadOrder(tx, order.UserID, order.OrderID)
			return err
		}
		if !CanTransitionOrder(order.Status, OrderStatusPaid) {
			return NewError(CodeInvalidTransition, "当前订单不能完成支付", nil)
		}
		payment.Status = PaymentStatusSucceeded
		payment.CallbackRef = &callbackRef
		payment.PaidAt = timePointer(now)
		payment.UpdatedAt = now
		if err := tx.Save(&payment).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return NewError(CodeConflict, "支付回调流水已使用", err)
			}
			return err
		}
		if err := r.confirmReservations(tx, order.OrderID, now); err != nil {
			return err
		}
		if err := r.transition(tx, &order, OrderStatusPaid, "Mock 支付成功", "payment", 0, now); err != nil {
			return err
		}
		if err := tx.Model(&order).Update("paid_at", now).Error; err != nil {
			return err
		}
		if err := r.appendOutbox(tx, order.OrderID, "commerce.payment.succeeded.v1", now, map[string]any{
			"order_id": order.OrderID, "payment_no": paymentNo, "amount_cents": payment.AmountCents,
		}); err != nil {
			return err
		}
		var err error
		result, err = r.loadOrder(tx, order.UserID, order.OrderID)
		return err
	})
	return result, err
}

func (r *MySQLRepository) ShipOrder(ctx context.Context, orderID, carrier, trackingNo string, actorID uint64, now time.Time) (Order, error) {
	var result Order
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order orderRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ?", orderID).First(&order).Error; err != nil {
			return mapNotFound(err, "订单不存在")
		}
		if order.Status == OrderStatusShipped {
			var err error
			result, err = r.loadOrder(tx, order.UserID, orderID)
			return err
		}
		if !CanTransitionOrder(order.Status, OrderStatusShipped) {
			return NewError(CodeInvalidTransition, "当前订单不能发货", nil)
		}
		shipment := shipmentRecord{
			OrderID: orderID, Carrier: carrier, TrackingNo: trackingNo,
			Status: ShipmentStatusShipped, ShippedAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&shipment).Error; err != nil {
			return err
		}
		if err := r.transition(tx, &order, OrderStatusShipped, "订单已发货", "admin", actorID, now); err != nil {
			return err
		}
		if err := tx.Model(&order).Update("shipped_at", now).Error; err != nil {
			return err
		}
		if err := r.appendOutbox(tx, orderID, "commerce.order.shipped.v1", now, map[string]any{
			"order_id": orderID, "carrier": carrier, "tracking_no": trackingNo,
		}); err != nil {
			return err
		}
		var err error
		result, err = r.loadOrder(tx, order.UserID, orderID)
		return err
	})
	return result, err
}

func (r *MySQLRepository) ConfirmOrder(ctx context.Context, userID uint64, orderID string, now time.Time) (Order, error) {
	var result Order
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order orderRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ? AND user_id = ?", orderID, userID).First(&order).Error; err != nil {
			return mapNotFound(err, "订单不存在")
		}
		if order.Status == OrderStatusCompleted {
			var err error
			result, err = r.loadOrder(tx, userID, orderID)
			return err
		}
		if !CanTransitionOrder(order.Status, OrderStatusCompleted) {
			return NewError(CodeInvalidTransition, "当前订单不能确认收货", nil)
		}
		if err := tx.Model(&shipmentRecord{}).Where("order_id = ?", orderID).Updates(map[string]any{
			"status": ShipmentStatusReceived, "delivered_at": now, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := r.transition(tx, &order, OrderStatusCompleted, "用户确认收货", "user", userID, now); err != nil {
			return err
		}
		if err := tx.Model(&order).Update("completed_at", now).Error; err != nil {
			return err
		}
		if err := r.appendOutbox(tx, orderID, "commerce.order.completed.v1", now, map[string]any{"order_id": orderID}); err != nil {
			return err
		}
		var err error
		result, err = r.loadOrder(tx, userID, orderID)
		return err
	})
	return result, err
}

func (r *MySQLRepository) RefundOrder(ctx context.Context, userID uint64, orderID, refundNo, reason string, now time.Time) (Refund, Order, error) {
	var refund Refund
	var result Order
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order orderRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ? AND user_id = ?", orderID, userID).First(&order).Error; err != nil {
			return mapNotFound(err, "订单不存在")
		}
		var existing refundRecord
		if err := tx.Where("order_id = ?", orderID).First(&existing).Error; err == nil {
			refund = refundFromRecord(existing)
			var loadErr error
			result, loadErr = r.loadOrder(tx, userID, orderID)
			return loadErr
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !CanTransitionOrder(order.Status, OrderStatusRefundPending) {
			return NewError(CodeInvalidTransition, "当前订单不能退款", nil)
		}
		var payment paymentRecord
		if err := tx.Where("order_id = ? AND status = ?", orderID, PaymentStatusSucceeded).First(&payment).Error; err != nil {
			return NewError(CodeInvalidTransition, "订单没有成功支付记录", err)
		}
		if err := r.transition(tx, &order, OrderStatusRefundPending, "申请退款: "+reason, "user", userID, now); err != nil {
			return err
		}
		if err := r.releaseReservations(tx, orderID, ReservationStatusReleased, now, true); err != nil {
			return err
		}
		record := refundRecord{
			RefundNo: refundNo, OrderID: orderID, PaymentNo: payment.PaymentNo,
			AmountCents: order.TotalAmountCents, Reason: reason, Status: RefundStatusSucceeded,
			CreatedAt: now, CompletedAt: timePointer(now),
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := r.transition(tx, &order, OrderStatusRefunded, "Mock 退款成功", "payment", 0, now); err != nil {
			return err
		}
		if err := tx.Model(&order).Update("refunded_at", now).Error; err != nil {
			return err
		}
		if err := r.appendOutbox(tx, orderID, "commerce.refund.succeeded.v1", now, map[string]any{
			"order_id": orderID, "refund_no": refundNo, "amount_cents": order.TotalAmountCents,
		}); err != nil {
			return err
		}
		refund = refundFromRecord(record)
		var loadErr error
		result, loadErr = r.loadOrder(tx, userID, orderID)
		return loadErr
	})
	return refund, result, err
}

func (r *MySQLRepository) requestedItems(tx *gorm.DB, command CreateOrderCommand) (map[uint64]int32, error) {
	requested := make(map[uint64]int32)
	if len(command.Items) == 0 {
		var carts []cartRecord
		if err := tx.Where("user_id = ?", command.UserID).Find(&carts).Error; err != nil {
			return nil, err
		}
		for _, cart := range carts {
			requested[cart.SKUID] += cart.Quantity
		}
	} else {
		for _, item := range command.Items {
			requested[item.SKUID] += item.Quantity
		}
	}
	if len(requested) == 0 {
		return nil, NewError(CodeValidation, "没有可结算商品", nil)
	}
	for _, quantity := range requested {
		if quantity <= 0 || quantity > 99 {
			return nil, NewError(CodeValidation, "商品数量无效", nil)
		}
	}
	return requested, nil
}

func (r *MySQLRepository) transition(tx *gorm.DB, order *orderRecord, to, reason, actorType string, actorID uint64, now time.Time) error {
	if !CanTransitionOrder(order.Status, to) {
		return NewError(CodeInvalidTransition, "订单状态转换无效", nil)
	}
	from := order.Status
	result := tx.Model(&orderRecord{}).Where("order_id = ? AND status = ?", order.OrderID, from).Updates(map[string]any{
		"status": to, "updated_at": now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return NewError(CodeConflict, "订单状态已变化，请重试", nil)
	}
	order.Status = to
	order.UpdatedAt = now
	return tx.Create(&statusRecord{
		OrderID: order.OrderID, FromStatus: from, ToStatus: to, Reason: reason,
		ActorType: actorType, ActorID: actorID, CreatedAt: now,
	}).Error
}

func (r *MySQLRepository) releaseReservations(tx *gorm.DB, orderID, targetStatus string, now time.Time, includeConfirmed bool) error {
	statuses := []string{ReservationStatusReserved}
	if includeConfirmed {
		statuses = append(statuses, ReservationStatusConfirmed)
	}
	var reservations []reservationRecord
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ? AND status IN ?", orderID, statuses).Order("sku_id ASC").Find(&reservations).Error; err != nil {
		return err
	}
	for _, reservation := range reservations {
		updates := map[string]any{"available_stock": gorm.Expr("available_stock + ?", reservation.Quantity), "updated_at": now}
		if reservation.Status == ReservationStatusReserved {
			updates["reserved_stock"] = gorm.Expr("reserved_stock - ?", reservation.Quantity)
		}
		if err := tx.Model(&skuRecord{}).Where("id = ?", reservation.SKUID).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Model(&reservationRecord{}).Where("id = ? AND status = ?", reservation.ID, reservation.Status).Updates(map[string]any{
			"status": targetStatus, "updated_at": now,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *MySQLRepository) confirmReservations(tx *gorm.DB, orderID string, now time.Time) error {
	var reservations []reservationRecord
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ? AND status = ?", orderID, ReservationStatusReserved).Order("sku_id ASC").Find(&reservations).Error; err != nil {
		return err
	}
	if len(reservations) == 0 {
		return NewError(CodeConflict, "订单库存预占不存在", nil)
	}
	for _, reservation := range reservations {
		result := tx.Model(&skuRecord{}).Where("id = ? AND reserved_stock >= ?", reservation.SKUID, reservation.Quantity).Updates(map[string]any{
			"reserved_stock": gorm.Expr("reserved_stock - ?", reservation.Quantity), "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return NewError(CodeConflict, "库存预占状态异常", nil)
		}
		if err := tx.Model(&reservationRecord{}).Where("id = ? AND status = ?", reservation.ID, ReservationStatusReserved).Updates(map[string]any{
			"status": ReservationStatusConfirmed, "updated_at": now,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *MySQLRepository) appendOutbox(tx *gorm.DB, aggregateID, eventType string, now time.Time, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	record := outboxRecord{
		EventID: outboxEventID(eventType, aggregateID), AggregateType: "order", AggregateID: aggregateID,
		EventType: eventType, EventVersion: 1, Payload: string(body), Status: "pending",
		NextRetryAt: now, CreatedAt: now, UpdatedAt: now,
	}
	return tx.Create(&record).Error
}

func outboxEventID(eventType, aggregateID string) string {
	sum := sha256.Sum256([]byte(eventType + ":" + aggregateID))
	return hex.EncodeToString(sum[:])
}

func (r *MySQLRepository) loadOrder(db *gorm.DB, userID uint64, orderID string) (Order, error) {
	var record orderRecord
	query := db.Where("order_id = ?", orderID)
	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.First(&record).Error; err != nil {
		return Order{}, mapNotFound(err, "订单不存在")
	}
	order, err := orderFromRecord(record)
	if err != nil {
		return Order{}, err
	}
	var items []orderItemRecord
	if err := db.Where("order_id = ?", orderID).Order("id ASC").Find(&items).Error; err != nil {
		return Order{}, err
	}
	for _, item := range items {
		order.Items = append(order.Items, orderItemFromRecord(item))
	}
	var history []statusRecord
	if err := db.Where("order_id = ?", orderID).Order("id ASC").Find(&history).Error; err != nil {
		return Order{}, err
	}
	for _, status := range history {
		order.StatusHistory = append(order.StatusHistory, statusFromRecord(status))
	}
	var payment paymentRecord
	if err := db.Where("order_id = ?", orderID).First(&payment).Error; err == nil {
		converted := paymentFromRecord(payment)
		order.Payment = &converted
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return Order{}, err
	}
	var shipment shipmentRecord
	if err := db.Where("order_id = ?", orderID).First(&shipment).Error; err == nil {
		converted := shipmentFromRecord(shipment)
		order.Shipment = &converted
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return Order{}, err
	}
	return order, nil
}

func (r *MySQLRepository) productsFromSPUs(ctx context.Context, records []spuRecord, includeInactive bool) ([]Product, error) {
	if len(records) == 0 {
		return []Product{}, nil
	}
	ids := make([]uint64, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	query := r.db.WithContext(ctx).Where("spu_id IN ?", ids)
	if !includeInactive {
		query = query.Where("active = ?", true)
	}
	var skus []skuRecord
	if err := query.Order("id ASC").Find(&skus).Error; err != nil {
		return nil, err
	}
	var images []productImageRecord
	if err := r.db.WithContext(ctx).Where("spu_id IN ?", ids).Order("sort_order ASC, id ASC").Find(&images).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint64]*Product, len(records))
	products := make([]Product, len(records))
	for index, record := range records {
		products[index].SPU = spuFromRecord(record)
		products[index].SKUs = []SKU{}
		products[index].Images = []string{}
		byID[record.ID] = &products[index]
	}
	for _, sku := range skus {
		if product := byID[sku.SPUID]; product != nil {
			product.SKUs = append(product.SKUs, skuFromRecord(sku))
		}
	}
	for _, image := range images {
		if product := byID[image.SPUID]; product != nil {
			product.Images = append(product.Images, image.URL)
		}
	}
	return products, nil
}

func (r *MySQLRepository) getProductIncludingInactive(ctx context.Context, productID uint64) (Product, error) {
	var record spuRecord
	if err := r.db.WithContext(ctx).First(&record, productID).Error; err != nil {
		return Product{}, mapNotFound(err, "商品不存在")
	}
	products, err := r.productsFromSPUs(ctx, []spuRecord{record}, true)
	if err != nil {
		return Product{}, err
	}
	return products[0], nil
}

func mapNotFound(err error, message string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewError(CodeNotFound, message, err)
	}
	return err
}

func userFromRecord(record userRecord) User {
	return User{ID: record.ID, Email: record.Email, PasswordHash: record.PasswordHash, Role: record.Role, Status: record.Status, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func addressToRecord(address Address) addressRecord {
	return addressRecord{ID: address.ID, UserID: address.UserID, Recipient: address.Recipient, Phone: address.Phone, Province: address.Province, City: address.City, District: address.District, Detail: address.Detail, IsDefault: address.IsDefault}
}

func addressFromRecord(record addressRecord) Address {
	return Address{ID: record.ID, UserID: record.UserID, Recipient: record.Recipient, Phone: record.Phone, Province: record.Province, City: record.City, District: record.District, Detail: record.Detail, IsDefault: record.IsDefault, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func spuFromRecord(record spuRecord) SPU {
	return SPU{ID: record.ID, CategoryID: record.CategoryID, Name: record.Name, Description: record.Description, Active: record.Active}
}

func skuFromRecord(record skuRecord) SKU {
	return SKU{ID: record.ID, SPUID: record.SPUID, Code: record.Code, Name: record.Name, PriceCents: record.PriceCents, AvailableStock: record.AvailableStock, ReservedStock: record.ReservedStock, Active: record.Active, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func cartFromRecords(cart cartRecord, sku skuRecord) CartItem {
	subtotal, _ := CalculateSubtotal(sku.PriceCents, cart.Quantity)
	return CartItem{ID: cart.ID, UserID: cart.UserID, SKUID: cart.SKUID, Quantity: cart.Quantity, SKU: skuFromRecord(sku), Subtotal: subtotal, CreatedAt: cart.CreatedAt, UpdatedAt: cart.UpdatedAt}
}

func orderFromRecord(record orderRecord) (Order, error) {
	var address Address
	if err := json.Unmarshal([]byte(record.AddressSnapshot), &address); err != nil {
		return Order{}, fmt.Errorf("解析订单地址快照: %w", err)
	}
	return Order{
		ID: record.ID, OrderID: record.OrderID, UserID: record.UserID, OrderType: record.OrderType,
		Status: record.Status, TotalAmountCents: record.TotalAmountCents, IdempotencyKey: record.IdempotencyKey,
		AddressSnapshot: address, ExpiresAt: record.ExpiresAt, PaidAt: record.PaidAt,
		ShippedAt: record.ShippedAt, CompletedAt: record.CompletedAt, CanceledAt: record.CanceledAt,
		RefundedAt: record.RefundedAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}, nil
}

func orderItemFromRecord(record orderItemRecord) OrderItem {
	return OrderItem{ID: record.ID, OrderID: record.OrderID, SKUID: record.SKUID, SKUCode: record.SKUCode, SKUName: record.SKUName, UnitPriceCents: record.UnitPriceCents, Quantity: record.Quantity, SubtotalCents: record.SubtotalCents}
}

func statusFromRecord(record statusRecord) OrderStatus {
	return OrderStatus{ID: record.ID, OrderID: record.OrderID, FromStatus: record.FromStatus, ToStatus: record.ToStatus, Reason: record.Reason, ActorType: record.ActorType, ActorID: record.ActorID, CreatedAt: record.CreatedAt}
}

func paymentFromRecord(record paymentRecord) Payment {
	callback := ""
	if record.CallbackRef != nil {
		callback = *record.CallbackRef
	}
	return Payment{ID: record.ID, PaymentNo: record.PaymentNo, OrderID: record.OrderID, AmountCents: record.AmountCents, Provider: record.Provider, Status: record.Status, CallbackRef: callback, PaidAt: record.PaidAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func refundFromRecord(record refundRecord) Refund {
	return Refund{ID: record.ID, RefundNo: record.RefundNo, OrderID: record.OrderID, PaymentNo: record.PaymentNo, AmountCents: record.AmountCents, Reason: record.Reason, Status: record.Status, CreatedAt: record.CreatedAt, CompletedAt: record.CompletedAt}
}

func shipmentFromRecord(record shipmentRecord) Shipment {
	return Shipment{ID: record.ID, OrderID: record.OrderID, Carrier: record.Carrier, TrackingNo: record.TrackingNo, Status: record.Status, ShippedAt: record.ShippedAt, DeliveredAt: record.DeliveredAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

var _ Repository = (*MySQLRepository)(nil)
