// 本文件保留 Order 包既有内部认证 API，并委托给跨服务通用实现。
package order

import (
	"context"
	"os"
	"strings"
	"time"

	"seckill-mall/shared/platform/internalcall"
)

const (
	internalSignatureKey = internalcall.SignatureKey
	systemActorID        = internalcall.SystemActorID
	systemRole           = internalcall.SystemRole
)

func AppendSignedMetadata(ctx context.Context, secret, method string, userID uint64, now time.Time) context.Context {
	return internalcall.AppendUser(ctx, secret, method, userID, now)
}

func AppendSignedRoleMetadata(ctx context.Context, secret, method string, userID uint64, role string, now time.Time) context.Context {
	return internalcall.AppendRole(ctx, secret, method, userID, role, now)
}

func AuthorizeInternalRequest(ctx context.Context, method string, userID uint64) error {
	return internalcall.AuthorizeUser(ctx, strings.TrimSpace(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")), method, userID, time.Now())
}

func AuthorizeInternalRoleRequest(ctx context.Context, method string, userID uint64, requiredRole string) error {
	return internalcall.AuthorizeRole(ctx, strings.TrimSpace(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")), method, userID, requiredRole, time.Now())
}
