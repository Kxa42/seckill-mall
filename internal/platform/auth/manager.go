// Package auth 提供密码哈希、访问令牌和刷新令牌能力。
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultIssuer = "seckill-commerce"
	accessType    = "access"
)

// Claims 是 commerce-api 使用的 JWT 载荷。
type Claims struct {
	UserID    uint64 `json:"user_id"`
	Role      string `json:"role"`
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
}

// Manager 管理访问令牌和刷新令牌。
type Manager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

// RefreshToken 包含只返回给客户端一次的原始令牌和可持久化哈希。
type RefreshToken struct {
	Raw       string
	Hash      string
	ExpiresAt time.Time
}

// NewManager 创建认证管理器。
func NewManager(secret string, accessTTL, refreshTTL time.Duration) (*Manager, error) {
	if len(strings.TrimSpace(secret)) < 32 {
		return nil, errors.New("JWT 密钥长度不能小于 32")
	}
	if accessTTL <= 0 || refreshTTL <= 0 {
		return nil, errors.New("令牌有效期必须大于 0")
	}
	return &Manager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		now:        time.Now,
	}, nil
}

// HashPassword 使用 bcrypt 哈希密码。
func HashPassword(password string) (string, error) {
	if len(password) < 8 || len(password) > 72 {
		return "", errors.New("密码长度必须在 8 到 72 个字符之间")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

// VerifyPassword 验证密码与哈希是否匹配。
func VerifyPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// IssueAccessToken 签发短期访问令牌。
func (m *Manager) IssueAccessToken(userID uint64, role string) (string, time.Time, error) {
	if userID == 0 || strings.TrimSpace(role) == "" {
		return "", time.Time{}, errors.New("用户和角色不能为空")
	}
	now := m.now().UTC()
	expiresAt := now.Add(m.accessTTL)
	claims := Claims{
		UserID:    userID,
		Role:      role,
		TokenType: accessType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    defaultIssuer,
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	return signed, expiresAt, err
}

// ParseAccessToken 验证访问令牌并返回 Claims。
func (m *Manager) ParseAccessToken(raw string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("不支持的签名算法: %s", token.Method.Alg())
			}
			return m.secret, nil
		},
		jwt.WithIssuer(defaultIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return nil, errors.New("访问令牌无效或已过期")
	}
	if claims.TokenType != accessType || claims.UserID == 0 || claims.Role == "" {
		return nil, errors.New("访问令牌载荷无效")
	}
	return claims, nil
}

// IssueRefreshToken 生成不可预测的刷新令牌及其哈希。
func (m *Manager) IssueRefreshToken() (RefreshToken, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return RefreshToken{}, fmt.Errorf("生成刷新令牌: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(buffer)
	return RefreshToken{
		Raw:       raw,
		Hash:      HashOpaqueToken(raw),
		ExpiresAt: m.now().UTC().Add(m.refreshTTL),
	}, nil
}

// HashOpaqueToken 返回适合持久化比较的不可逆令牌哈希。
func HashOpaqueToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
