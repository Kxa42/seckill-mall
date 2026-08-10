package identityservice

import (
	"time"
)

const (
	RoleCustomer = "customer"
	RoleAdmin    = "admin"
	StatusActive = "active"
)

type User struct {
	ID           uint64
	Email        string
	PasswordHash string
	Role         string
	Status       string
}

type RefreshToken struct {
	UserID    uint64
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type Address struct {
	ID        uint64
	UserID    uint64
	Recipient string
	Phone     string
	Province  string
	City      string
	District  string
	Detail    string
	IsDefault bool
}
