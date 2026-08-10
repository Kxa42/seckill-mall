package gateway

import (
	"github.com/gin-gonic/gin"
	ginprometheus "github.com/zsais/go-gin-prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
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
}
