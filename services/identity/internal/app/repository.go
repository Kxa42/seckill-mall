// Package identityservice 提供 Identity 服务的数据所有权边界。
// 本包只访问 users、refresh_tokens 和 user_addresses，不依赖 Commerce Repository。
package identityservice

import (
	"context"
	"errors"
	"time"
)

var (
	ErrAddressNotFound = errors.New("identity address not found")
	ErrUserNotFound    = errors.New("identity user not found")
	ErrEmailConflict   = errors.New("identity email already exists")
	ErrTokenInvalid    = errors.New("identity refresh token invalid")
)

type Repository interface {
	CreateUser(ctx context.Context, email, passwordHash, role string, now time.Time) (User, error)
	FindUserByEmail(ctx context.Context, email string) (User, error)
	StoreRefreshToken(ctx context.Context, token RefreshToken) error
	RotateRefreshToken(ctx context.Context, oldHash string, replacement RefreshToken, now time.Time) (User, error)
	EnsureAdmin(ctx context.Context, email, passwordHash string, now time.Time) error
	CreateAddress(ctx context.Context, address Address, now time.Time) (Address, error)
	UpdateAddress(ctx context.Context, address Address, now time.Time) (Address, error)
	DeleteAddress(ctx context.Context, userID, addressID uint64) error
	ListAddresses(ctx context.Context, userID uint64) ([]Address, error)
	GetAddress(ctx context.Context, userID, addressID uint64) (Address, error)
}
