// 本文件提供新商城 /api/v1 路由使用的严格 JWT 验证。
// 旧秒杀接口继续使用 auth.go 中的兼容 Token，避免破坏历史客户端。
package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"seckill-mall/common/config"
	platformauth "seckill-mall/internal/platform/auth"
	"seckill-mall/internal/platform/httpx"
)

const UserRoleKey = "userRole"

// CommerceJWTAuth 验证 Identity Service 签发的商城 Access Token。
func CommerceJWTAuth() gin.HandlerFunc {
	secret := ""
	if config.Conf != nil {
		secret = strings.TrimSpace(config.Conf.JWT.Secret)
	}
	manager, managerErr := platformauth.NewManager(secret, time.Minute, time.Hour)
	return func(c *gin.Context) {
		if managerErr != nil {
			httpx.Error(c, http.StatusServiceUnavailable, "AUTH_NOT_READY", "认证服务配置不可用")
			return
		}
		parts := strings.SplitN(strings.TrimSpace(c.GetHeader("Authorization")), " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			httpx.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "请提供 Bearer 访问令牌")
			return
		}
		claims, err := manager.ParseAccessToken(parts[1])
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "访问令牌无效或已过期")
			return
		}
		c.Set("userID", int64(claims.UserID))
		c.Set(UserRoleKey, claims.Role)
		c.Next()
	}
}
