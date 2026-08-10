package identityservice

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu            sync.RWMutex
	nextUserID    uint64
	nextAddressID uint64
	users         map[uint64]User
	usersByEmail  map[string]uint64
	refreshTokens map[string]RefreshToken
	addresses     map[uint64]Address
}

func NewMemoryRepository(addresses ...Address) *MemoryRepository {
	r := &MemoryRepository{
		nextUserID:    1,
		nextAddressID: 1,
		users:         make(map[uint64]User),
		usersByEmail:  make(map[string]uint64),
		refreshTokens: make(map[string]RefreshToken),
		addresses:     make(map[uint64]Address),
	}
	for _, address := range addresses {
		if address.ID == 0 {
			address.ID = r.nextAddressID
		}
		if address.ID >= r.nextAddressID {
			r.nextAddressID = address.ID + 1
		}
		r.addresses[address.ID] = address
	}
	return r
}

func (r *MemoryRepository) nextUser() uint64 {
	value := r.nextUserID
	r.nextUserID++
	return value
}

func (r *MemoryRepository) nextAddress() uint64 {
	value := r.nextAddressID
	r.nextAddressID++
	return value
}

func (r *MemoryRepository) CreateUser(_ context.Context, email, passwordHash, role string, _ time.Time) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	if _, exists := r.usersByEmail[email]; exists {
		return User{}, ErrEmailConflict
	}
	user := User{ID: r.nextUser(), Email: email, PasswordHash: passwordHash, Role: role, Status: StatusActive}
	r.users[user.ID] = user
	r.usersByEmail[email] = user.ID
	return user, nil
}

func (r *MemoryRepository) FindUserByEmail(_ context.Context, email string) (User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, exists := r.usersByEmail[strings.ToLower(strings.TrimSpace(email))]
	if !exists {
		return User{}, ErrUserNotFound
	}
	return r.users[id], nil
}

func (r *MemoryRepository) StoreRefreshToken(_ context.Context, token RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshTokens[token.TokenHash] = token
	return nil
}

func (r *MemoryRepository) RotateRefreshToken(_ context.Context, oldHash string, replacement RefreshToken, now time.Time) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, exists := r.refreshTokens[oldHash]
	if !exists || existing.ExpiresAt.Before(now) {
		return User{}, ErrTokenInvalid
	}
	user, exists := r.users[existing.UserID]
	if !exists || user.Status != StatusActive {
		return User{}, ErrUserNotFound
	}
	delete(r.refreshTokens, oldHash)
	replacement.UserID = existing.UserID
	r.refreshTokens[replacement.TokenHash] = replacement
	return user, nil
}

func (r *MemoryRepository) EnsureAdmin(_ context.Context, email, passwordHash string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	if id, exists := r.usersByEmail[email]; exists {
		user := r.users[id]
		user.PasswordHash, user.Role, user.Status = passwordHash, RoleAdmin, StatusActive
		r.users[id] = user
		return nil
	}
	user := User{ID: r.nextUser(), Email: email, PasswordHash: passwordHash, Role: RoleAdmin, Status: StatusActive}
	r.users[user.ID], r.usersByEmail[email] = user, user.ID
	return nil
}

func (r *MemoryRepository) CreateAddress(_ context.Context, address Address, _ time.Time) (Address, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	address.ID = r.nextAddress()
	if address.IsDefault {
		r.clearDefaults(address.UserID, address.ID)
	}
	r.addresses[address.ID] = address
	return address, nil
}

func (r *MemoryRepository) UpdateAddress(_ context.Context, address Address, _ time.Time) (Address, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.addresses[address.ID]
	if !exists || current.UserID != address.UserID {
		return Address{}, ErrAddressNotFound
	}
	if address.IsDefault {
		r.clearDefaults(address.UserID, address.ID)
	}
	r.addresses[address.ID] = address
	return address, nil
}

func (r *MemoryRepository) DeleteAddress(_ context.Context, userID, addressID uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	address, exists := r.addresses[addressID]
	if !exists || address.UserID != userID {
		return ErrAddressNotFound
	}
	delete(r.addresses, addressID)
	return nil
}

func (r *MemoryRepository) ListAddresses(_ context.Context, userID uint64) ([]Address, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Address, 0)
	for _, address := range r.addresses {
		if address.UserID == userID {
			result = append(result, address)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].IsDefault != result[j].IsDefault {
			return result[i].IsDefault
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (r *MemoryRepository) GetAddress(_ context.Context, userID, addressID uint64) (Address, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.addresses[addressID]
	if !ok || value.UserID != userID {
		return Address{}, ErrAddressNotFound
	}
	return value, nil
}

func (r *MemoryRepository) clearDefaults(userID, exceptID uint64) {
	for id, address := range r.addresses {
		if address.UserID == userID && id != exceptID && address.IsDefault {
			address.IsDefault = false
			r.addresses[id] = address
		}
	}
}

var _ Repository = (*MemoryRepository)(nil)
