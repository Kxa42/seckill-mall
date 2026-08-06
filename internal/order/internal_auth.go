package order

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	internalUserKey      = "x-order-user-id"
	internalRoleKey      = "x-order-role"
	internalTimestampKey = "x-order-timestamp"
	internalSignatureKey = "x-order-signature"
	internalClockSkew    = 30 * time.Second
	systemActorID        = uint64(1)
	systemRole           = "system"
)

// AppendSignedMetadata 在配置内部调用密钥后为 Gateway 请求添加可验证身份。
// 密钥为空时保留本地 Fake/Memory 调试能力，不把开发环境伪装成生产安全边界。
func AppendSignedMetadata(ctx context.Context, secret, method string, userID uint64, now time.Time) context.Context {
	secret = strings.TrimSpace(secret)
	if secret == "" || userID == 0 {
		return ctx
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	signature := sign(secret, method, strconv.FormatUint(userID, 10), timestamp)
	return metadata.AppendToOutgoingContext(ctx, internalUserKey, strconv.FormatUint(userID, 10), internalTimestampKey, timestamp, internalSignatureKey, signature)
}

// AppendSignedRoleMetadata 为需要角色边界的内部调用添加签名身份。
func AppendSignedRoleMetadata(ctx context.Context, secret, method string, userID uint64, role string, now time.Time) context.Context {
	secret = strings.TrimSpace(secret)
	role = strings.TrimSpace(role)
	if secret == "" || userID == 0 || role == "" {
		return ctx
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	userValue := strconv.FormatUint(userID, 10)
	signature := signRole(secret, method, userValue, role, timestamp)
	return metadata.AppendToOutgoingContext(ctx, internalUserKey, userValue, internalRoleKey, role, internalTimestampKey, timestamp, internalSignatureKey, signature)
}

func AuthorizeInternalRequest(ctx context.Context, method string, userID uint64) error {
	secret := strings.TrimSpace(os.Getenv("SECKILL_INTERNAL_CALL_SECRET"))
	if secret == "" {
		return nil
	}
	values, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.PermissionDenied, "缺少内部调用身份")
	}
	providedUser := firstMetadata(values, internalUserKey)
	timestampValue := firstMetadata(values, internalTimestampKey)
	providedSignature := firstMetadata(values, internalSignatureKey)
	parsedUser, err := strconv.ParseUint(providedUser, 10, 64)
	parsedTimestamp, timeErr := strconv.ParseInt(timestampValue, 10, 64)
	if err != nil || timeErr != nil || parsedUser == 0 || parsedUser != userID || providedSignature == "" {
		return status.Error(codes.PermissionDenied, "内部调用身份无效")
	}
	requestTime := time.Unix(parsedTimestamp, 0)
	if time.Since(requestTime) > internalClockSkew || time.Until(requestTime) > internalClockSkew {
		return status.Error(codes.PermissionDenied, "内部调用身份已过期")
	}
	expected := sign(secret, method, providedUser, timestampValue)
	if !hmac.Equal([]byte(expected), []byte(providedSignature)) {
		return status.Error(codes.PermissionDenied, "内部调用签名无效")
	}
	return nil
}

// AuthorizeInternalRoleRequest 校验带角色声明的内部调用，避免普通用户身份被当作管理员使用。
func AuthorizeInternalRoleRequest(ctx context.Context, method string, userID uint64, requiredRole string) error {
	secret := strings.TrimSpace(os.Getenv("SECKILL_INTERNAL_CALL_SECRET"))
	if secret == "" {
		return nil
	}
	values, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.PermissionDenied, "缺少内部调用身份")
	}
	providedUser := firstMetadata(values, internalUserKey)
	providedRole := firstMetadata(values, internalRoleKey)
	timestampValue := firstMetadata(values, internalTimestampKey)
	providedSignature := firstMetadata(values, internalSignatureKey)
	parsedUser, err := strconv.ParseUint(providedUser, 10, 64)
	parsedTimestamp, timeErr := strconv.ParseInt(timestampValue, 10, 64)
	if err != nil || timeErr != nil || parsedUser == 0 || parsedUser != userID || providedRole != requiredRole || providedSignature == "" {
		return status.Error(codes.PermissionDenied, "内部调用角色无效")
	}
	requestTime := time.Unix(parsedTimestamp, 0)
	if time.Since(requestTime) > internalClockSkew || time.Until(requestTime) > internalClockSkew {
		return status.Error(codes.PermissionDenied, "内部调用身份已过期")
	}
	expected := signRole(secret, method, providedUser, providedRole, timestampValue)
	if !hmac.Equal([]byte(expected), []byte(providedSignature)) {
		return status.Error(codes.PermissionDenied, "内部调用签名无效")
	}
	return nil
}

func sign(secret, method, userID, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%s|%s|%s", method, userID, timestamp)
	return hex.EncodeToString(mac.Sum(nil))
}

func signRole(secret, method, userID, role, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%s|%s|%s|%s", method, userID, role, timestamp)
	return hex.EncodeToString(mac.Sum(nil))
}

func firstMetadata(values metadata.MD, key string) string {
	if items := values.Get(key); len(items) > 0 {
		return strings.TrimSpace(items[0])
	}
	return ""
}
