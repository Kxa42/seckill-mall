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

	// ✅ 1. 必须引入配置包
	"seckill-mall/common/config"
	"seckill-mall/common/pb"
)

func main() {
	// ✅ 2. 第一件事：必须先加载配置！
	// 如果不加这行，下面的 config.Conf 都是空的，程序直接崩
	config.InitConfig()

	// ✅ 3. 使用配置里的 Etcd 地址
	etcdAddr := config.Conf.Etcd.Addr

	// 1. 初始化 Etcd 连接
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

	// 2. 连接【商品服务】
	connProduct, err := grpc.Dial(
		"etcd:///seckill/product",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(etcdResolver),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)
	if err != nil {
		log.Fatalf("无法连接商品服务: %v", err)
	}
	productClient := pb.NewProductServiceClient(connProduct)

	// 3. 连接【订单服务】
	connOrder, err := grpc.Dial(
		"etcd:///seckill/order",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(etcdResolver),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)
	if err != nil {
		log.Fatalf("无法连接订单服务: %v", err)
	}
	orderClient := pb.NewOrderServiceClient(connOrder)

	// 4. 启动 Gin
	r := gin.Default()

	// 接口 1: 查询商品
	r.GET("/product/:id", func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		resp, err := productClient.GetProduct(context.Background(), &pb.ProductRequest{ProductId: id})
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"data": resp})
	})

	// 接口 2: 下单
	r.POST("/order", func(c *gin.Context) {
		var req struct {
			UserID    int64 `json:"user_id"`
			ProductID int64 `json:"product_id"`
			Count     int32 `json:"count"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "参数错误"})
			return
		}

		resp, err := orderClient.CreateOrder(context.Background(), &pb.CreateOrderRequest{
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
