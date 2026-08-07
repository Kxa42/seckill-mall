// Package internalcall 提供服务间 HMAC 身份签名与校验。
// 签名绑定完整 gRPC 方法、调用身份、角色和时间戳，避免 Gateway 身份被下游盲目信任。
package internalcall

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	UserKey      = "x-internal-user-id"
	RoleKey      = "x-internal-role"
	TimestampKey = "x-internal-timestamp"
	SignatureKey = "x-internal-signature"
	ClockSkew    = 30 * time.Second

	SystemActorID = uint64(1)
	SystemRole    = "system"
)

// ValidateSecret 校验生产内部调用密钥，避免空密钥触发本地 Fake 绕过语义。
func ValidateSecret(secret string) error {
	if len(strings.TrimSpace(secret)) < 32 {
		return fmt.Errorf("internal call secret length must be at least 32")
	}
	return nil
}

// AppendUser 为用户身份内部请求追加 HMAC metadata。
func AppendUser(ctx context.Context, secret, method string, userID uint64, now time.Time) context.Context {
	secret = strings.TrimSpace(secret)
	if secret == "" || userID == 0 {
		return ctx
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	user := strconv.FormatUint(userID, 10)
	signature := sign(secret, method, user, "", timestamp)
	return metadata.AppendToOutgoingContext(ctx, UserKey, user, TimestampKey, timestamp, SignatureKey, signature)
}

// AppendRole 为带角色的内部请求追加 HMAC metadata。
func AppendRole(ctx context.Context, secret, method string, userID uint64, role string, now time.Time) context.Context {
	secret = strings.TrimSpace(secret)
	role = strings.TrimSpace(role)
	if secret == "" || userID == 0 || role == "" {
		return ctx
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	user := strconv.FormatUint(userID, 10)
	signature := sign(secret, method, user, role, timestamp)
	return metadata.AppendToOutgoingContext(ctx, UserKey, user, RoleKey, role, TimestampKey, timestamp, SignatureKey, signature)
}

// AuthorizeUser 校验请求中的用户身份和时间窗口。secret 为空只允许本地 Fake 环境绕过。
func AuthorizeUser(ctx context.Context, secret, method string, userID uint64, now time.Time) error {
	if strings.TrimSpace(secret) == "" {
		return nil
	}
	user, role, timestamp, signature, err := metadataValues(ctx)
	if err != nil || role != "" || user != userID {
		return status.Error(codes.PermissionDenied, "内部调用身份无效")
	}
	if err := validateTimestamp(timestamp, now); err != nil {
		return err
	}
	expected := sign(secret, method, strconv.FormatUint(user, 10), "", strconv.FormatInt(timestamp, 10))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return status.Error(codes.PermissionDenied, "内部调用签名无效")
	}
	return nil
}

// AuthorizeRole 校验请求中的用户和角色，避免普通用户伪造管理员或系统身份。
func AuthorizeRole(ctx context.Context, secret, method string, userID uint64, requiredRole string, now time.Time) error {
	if strings.TrimSpace(secret) == "" {
		return nil
	}
	user, role, timestamp, signature, err := metadataValues(ctx)
	if err != nil || user != userID || role != requiredRole {
		return status.Error(codes.PermissionDenied, "内部调用角色无效")
	}
	if err := validateTimestamp(timestamp, now); err != nil {
		return err
	}
	expected := sign(secret, method, strconv.FormatUint(user, 10), role, strconv.FormatInt(timestamp, 10))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return status.Error(codes.PermissionDenied, "内部调用签名无效")
	}
	return nil
}

func metadataValues(ctx context.Context) (uint64, string, int64, string, error) {
	values, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return 0, "", 0, "", status.Error(codes.PermissionDenied, "缺少内部调用身份")
	}
	user, userErr := strconv.ParseUint(first(values, UserKey), 10, 64)
	timestamp, timeErr := strconv.ParseInt(first(values, TimestampKey), 10, 64)
	signature := first(values, SignatureKey)
	if userErr != nil || timeErr != nil || user == 0 || signature == "" {
		return 0, "", 0, "", status.Error(codes.PermissionDenied, "内部调用身份无效")
	}
	return user, first(values, RoleKey), timestamp, signature, nil
}

func validateTimestamp(timestamp int64, now time.Time) error {
	requestTime := time.Unix(timestamp, 0)
	if now.Sub(requestTime) > ClockSkew || requestTime.Sub(now) > ClockSkew {
		return status.Error(codes.PermissionDenied, "内部调用身份已过期")
	}
	return nil
}

func sign(secret, method, userID, role, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(secret)))
	_, _ = fmt.Fprintf(mac, "%s|%s|%s|%s", method, userID, role, timestamp)
	return hex.EncodeToString(mac.Sum(nil))
}

func first(values metadata.MD, key string) string {
	items := values.Get(key)
	if len(items) == 0 {
		return ""
	}
	return strings.TrimSpace(items[0])
}
