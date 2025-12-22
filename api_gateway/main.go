package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	clientv3 "go.etcd.io/etcd/client/v3"
	resolver "go.etcd.io/etcd/client/v3/naming/resolver"

	"seckill-mall/common/config"
	"seckill-mall/common/pb"
	"seckill-mall/common/tracer"

	"seckill-mall/api_gateway/middleware"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/flow"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

func initSentinel() {
	// 初始化 Sentinel
	err := sentinel.InitDefault()
	if err != nil {
		log.Fatalf("初始化 Sentinel 失败: %v", err)
	}

	// 配置限流规则
	_, err = flow.LoadRules([]*flow.Rule{
		{
			Resource:               "create_order", // 资源名称
			TokenCalculateStrategy: flow.Direct,    //直接计数
			ControlBehavior:        flow.Reject,    //直接拒绝
			Threshold:              5,              // 为了测试暂定每秒允许的最大请求数为5
			StatIntervalInMs:       1000,           // 统计周期1秒
		},
	})
	if err != nil {
		log.Fatalf("加载限流规则失败: %v", err)
	}
	log.Println("Sentinel限流规则已加载：create_order 每秒最大请求数 5")
}

func main() {
	// 先加载配置
	config.InitConfig()

	//初始化链路追踪
	shutdown := tracer.InitTracer("api-gateway", "localhost:4318")
	defer shutdown(context.Background())

	// 初始化 Sentinel
	initSentinel()

	// 使用配置里的 Etcd 地址
	etcdAddr := config.Conf.Etcd.Addr

	// 初始化 Etcd 连接
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdAddr}, // 使用配置变量
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("连接 Etcd 失败: %v", err)
	}
	etcdResolver, err := resolver.NewBuilder(cli)
	if err != nil {
		log.Fatalf("创建解析器失败: %v", err)
	}

	// 连接【商品服务】
	connProduct, err := grpc.Dial(
		"etcd:///seckill/product",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithResolvers(etcdResolver),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)
	if err != nil {
		log.Fatalf("无法连接商品服务: %v", err)
	}
	productClient := pb.NewProductServiceClient(connProduct)

	// 连接【订单服务】
	connOrder, err := grpc.Dial(
		"etcd:///seckill/order",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithResolvers(etcdResolver),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)
	if err != nil {
		log.Fatalf("无法连接订单服务: %v", err)
	}
	orderClient := pb.NewOrderServiceClient(connOrder)

	// 启动 Gin
	r := gin.Default()

	//添加Gin中间件，自动记录http请求
	r.Use(otelgin.Middleware("api-gateway"))

	// 接口 1: 查询商品
	r.GET("/product/:id", func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		resp, err := productClient.GetProduct(c.Request.Context(), &pb.ProductRequest{ProductId: id})
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"data": resp})
	})

	// 接口 2: 下单
	r.POST("/order", middleware.SentinelLimit("create_order"), func(c *gin.Context) {
		var req struct {
			UserID    int64 `json:"user_id"`
			ProductID int64 `json:"product_id"`
			Count     int32 `json:"count"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "参数错误"})
			return
		}

		resp, err := orderClient.CreateOrder(c.Request.Context(), &pb.CreateOrderRequest{
			UserId:    req.UserID,
			ProductId: req.ProductID,
			Count:     req.Count,
		})

		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"code":    200,
			"message": "下单成功",
			"data":    resp,
		})
	})

	fmt.Println("=== API 网关已启动 (Port: 8080) ===")
	r.Run(":8080")
}
