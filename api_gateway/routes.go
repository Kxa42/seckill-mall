package main

import (
	"time"

	"github.com/gin-gonic/gin"
	ginprometheus "github.com/zsais/go-gin-prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"seckill-mall/common/config"
	"seckill-mall/common/utils"
)

func setupRouter(clients grpcClients) *gin.Engine {
	r := gin.Default()
	p := ginprometheus.NewPrometheus("gin")
	p.Use(r)
	r.Use(otelgin.Middleware("api-gateway"))
	registerRoutes(r, clients)
	return r
}

func registerRoutes(r *gin.Engine, clients grpcClients) {
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	registerCatalogRoutes(r, clients.catalog)
	registerIdentityRoutes(r, clients.identity)
	registerCartRoutes(r, clients.cart)
	registerOrderRoutes(r, clients.commerceOrder, clients.cart)
	registerPaymentRoutes(r, clients.payment)
	registerFulfillmentRoutes(r, clients.fulfillment)

	// 仅保留本地 debug 登录辅助，业务读写必须走版本化 API。
	if config.Conf.Server.Mode == "debug" {
		r.POST("/login", func(c *gin.Context) {
			var req struct {
				UserID int64 `json:"user_id"`
			}
			if err := c.ShouldBind(&req); err != nil {
				c.JSON(400, gin.H{"error": "参数错误"})
				return
			}
			expireStr := config.Conf.JWT.Expire
			expireDuration, err := time.ParseDuration(expireStr)
			if err != nil {
				expireDuration = 2 * time.Hour
			}
			token, err := utils.GenerateToken(req.UserID, expireDuration)
			if err != nil {
				c.JSON(500, gin.H{"error": "生成Token失败"})
				return
			}
			c.JSON(200, gin.H{"code": 200, "message": "登录成功", "token": token, "expire": expireStr})
		})
	}
}
