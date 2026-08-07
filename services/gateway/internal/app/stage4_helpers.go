// 本文件提供阶段 4 Gateway 路由共用的内部调用签名、超时和错误映射。
package gateway

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/httpx"
	"seckill-mall/shared/platform/internalcall"
)

const stage4RPCTimeout = 3 * time.Second

func signedUserContext(c *gin.Context, method string, userID uint64) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), stage4RPCTimeout)
	return internalcall.AppendUser(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), method, userID, time.Now()), cancel
}

func signedSystemContext(c *gin.Context, method string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), stage4RPCTimeout)
	return internalcall.AppendRole(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), method, internalcall.SystemActorID, internalcall.SystemRole, time.Now()), cancel
}

func signedAdminContext(c *gin.Context, method string, userID uint64) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), stage4RPCTimeout)
	return internalcall.AppendRole(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), method, userID, "admin", time.Now()), cancel
}

func respondStage4RPCError(c *gin.Context, err error, resource string) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", resource+"请求参数无效")
	case codes.Unauthenticated:
		httpx.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "认证信息无效")
	case codes.PermissionDenied:
		httpx.Error(c, http.StatusForbidden, "FORBIDDEN", "权限不足")
	case codes.NotFound:
		httpx.Error(c, http.StatusNotFound, "NOT_FOUND", resource+"不存在")
	case codes.AlreadyExists:
		httpx.Error(c, http.StatusConflict, "ALREADY_EXISTS", resource+"已存在")
	case codes.Aborted:
		httpx.Error(c, http.StatusConflict, "CONFLICT", resource+"操作冲突")
	case codes.FailedPrecondition:
		httpx.Error(c, http.StatusConflict, "INVALID_STATE", resource+"当前状态不允许该操作")
	case codes.Unavailable, codes.DeadlineExceeded:
		httpx.Error(c, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", resource+"服务暂时不可用")
	default:
		httpx.Error(c, http.StatusBadGateway, "UPSTREAM_ERROR", resource+"服务暂时不可用")
	}
}

func addressRequest(value *pb.IdentityAddress) *pb.IdentityAddress {
	if value == nil {
		return &pb.IdentityAddress{}
	}
	return value
}
