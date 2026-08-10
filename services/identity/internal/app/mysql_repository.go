package identityservice

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type MySQLRepository struct{ db *gorm.DB }

type userRecord struct {
	ID           uint64 `gorm:"column:id;primaryKey"`
	Email        string
	PasswordHash string `gorm:"column:password_hash"`
	Role         string
	Status       string
}

func (userRecord) TableName() string { return "users" }

type refreshTokenRecord struct {
	ID        uint64     `gorm:"column:id;primaryKey"`
	UserID    uint64     `gorm:"column:user_id"`
	TokenHash string     `gorm:"column:token_hash"`
	ExpiresAt time.Time  `gorm:"column:expires_at"`
	RevokedAt *time.Time `gorm:"column:revoked_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
}

func (refreshTokenRecord) TableName() string { return "refresh_tokens" }

type addressRecord struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	UserID    uint64 `gorm:"column:user_id"`
	Recipient string
	Phone     string
	Province  string
	City      string
	District  string
	Detail    string
	IsDefault bool `gorm:"column:is_default"`
}

func (addressRecord) TableName() string { return "user_addresses" }

func NewMySQLRepository(db *gorm.DB) (*MySQLRepository, error) {
	if db == nil {
		return nil, errors.New("identity db is required")
	}
	return &MySQLRepository{db: db}, nil
}

func userFromRecord(value userRecord) User {
	return User{ID: value.ID, Email: value.Email, PasswordHash: value.PasswordHash, Role: value.Role, Status: value.Status}
}

func addressFromRecord(value addressRecord) Address {
	return Address{ID: value.ID, UserID: value.UserID, Recipient: value.Recipient, Phone: value.Phone, Province: value.Province, City: value.City, District: value.District, Detail: value.Detail, IsDefault: value.IsDefault}
}

func addressRecordFromValue(value Address, now time.Time) addressRecord {
	return addressRecord{ID: value.ID, UserID: value.UserID, Recipient: value.Recipient, Phone: value.Phone, Province: value.Province, City: value.City, District: value.District, Detail: value.Detail, IsDefault: value.IsDefault}
}

func (r *MySQLRepository) CreateUser(ctx context.Context, email, passwordHash, role string, _ time.Time) (User, error) {
	record := userRecord{Email: email, PasswordHash: passwordHash, Role: role, Status: StatusActive}
	if err := r.db.WithContext(ctx).Create(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return User{}, ErrEmailConflict
		}
		return User{}, err
	}
	return userFromRecord(record), nil
}

func (r *MySQLRepository) FindUserByEmail(ctx context.Context, email string) (User, error) {
	var record userRecord
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}
	return userFromRecord(record), nil
}

func (r *MySQLRepository) StoreRefreshToken(ctx context.Context, token RefreshToken) error {
	record := refreshTokenRecord{UserID: token.UserID, TokenHash: token.TokenHash, ExpiresAt: token.ExpiresAt, CreatedAt: token.CreatedAt}
	return r.db.WithContext(ctx).Create(&record).Error
}

func (r *MySQLRepository) RotateRefreshToken(ctx context.Context, oldHash string, replacement RefreshToken, now time.Time) (User, error) {
	var result User
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var token refreshTokenRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", oldHash, now).First(&token).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTokenInvalid
			}
			return err
		}
		if err := tx.Model(&token).Update("revoked_at", now).Error; err != nil {
			return err
		}
		var user userRecord
		if err := tx.Where("id = ? AND status = ?", token.UserID, StatusActive).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}
		replacementRecord := refreshTokenRecord{UserID: token.UserID, TokenHash: replacement.TokenHash, ExpiresAt: replacement.ExpiresAt, CreatedAt: replacement.CreatedAt}
		if err := tx.Create(&replacementRecord).Error; err != nil {
			return err
		}
		result = userFromRecord(user)
		return nil
	})
	return result, err
}

func (r *MySQLRepository) EnsureAdmin(ctx context.Context, email, passwordHash string, now time.Time) error {
	var user userRecord
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err == nil {
		return r.db.WithContext(ctx).Model(&user).Updates(map[string]any{"password_hash": passwordHash, "role": RoleAdmin, "status": StatusActive, "updated_at": now}).Error
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return r.db.WithContext(ctx).Create(&userRecord{Email: email, PasswordHash: passwordHash, Role: RoleAdmin, Status: StatusActive}).Error
}

func (r *MySQLRepository) CreateAddress(ctx context.Context, address Address, now time.Time) (Address, error) {
	var result Address
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if address.IsDefault {
			if err := tx.Model(&addressRecord{}).Where("user_id = ?", address.UserID).Updates(map[string]any{"is_default": false, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		record := addressRecordFromValue(address, now)
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		result = addressFromRecord(record)
		return nil
	})
	return result, err
}

func (r *MySQLRepository) UpdateAddress(ctx context.Context, address Address, now time.Time) (Address, error) {
	var result Address
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current addressRecord
		if err := tx.Where("id = ? AND user_id = ?", address.ID, address.UserID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAddressNotFound
			}
			return err
		}
		if address.IsDefault {
			if err := tx.Model(&addressRecord{}).Where("user_id = ? AND id <> ?", address.UserID, address.ID).Updates(map[string]any{"is_default": false, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&current).Updates(map[string]any{"recipient": address.Recipient, "phone": address.Phone, "province": address.Province, "city": address.City, "district": address.District, "detail": address.Detail, "is_default": address.IsDefault, "updated_at": now}).Error; err != nil {
			return err
		}
		result = address
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
		return ErrAddressNotFound
	}
	return nil
}

func (r *MySQLRepository) ListAddresses(ctx context.Context, userID uint64) ([]Address, error) {
	var records []addressRecord
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("is_default DESC, id ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]Address, 0, len(records))
	for _, record := range records {
		result = append(result, addressFromRecord(record))
	}
	return result, nil
}

func (r *MySQLRepository) GetAddress(ctx context.Context, userID, addressID uint64) (Address, error) {
	var record addressRecord
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", addressID, userID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Address{}, ErrAddressNotFound
		}
		return Address{}, err
	}
	return addressFromRecord(record), nil
}

var _ Repository = (*MySQLRepository)(nil)
