package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/endpoints"
	resolver "go.etcd.io/etcd/client/v3/naming/resolver"

	"seckill-mall/common/config"
	"seckill-mall/common/pb"
)

const (
	SERVICE_NAME = "seckill/order"
	SERVICE_ADDR = "127.0.0.1:50052"

	PRODUCT_SERVICE_NAME = "etcd:///seckill/product"
)

// 数据库模型
type Order struct {
	ID        int64     `gorm:"primaryKey"`
	OrderID   string    `gorm:"type:varchar(64)"`
	UserID    int64     `gorm:"type:bigint"`
	ProductID int64     `gorm:"type:bigint"`
	Amount    float32   `gorm:"type:decimal(10,2)"`
	Status    int32     `gorm:"type:int"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (Order) TableName() string { return "orders" }

var db *gorm.DB
var productClient pb.ProductServiceClient

type server struct {
	pb.UnimplementedOrderServiceServer
}

// CreateOrder 下单逻辑 (保持不变)
func (s *server) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.CreateOrderResponse, error) {
	fmt.Printf("收到下单请求，用户: %d, 商品: %d\n", req.UserId, req.ProductId)

	// 1. 扣减 Redis 库存
	deductResp, err := productClient.DeductStock(context.Background(), &pb.DeductStockRequest{
		ProductId: req.ProductId,
		Count:     req.Count,
	})
	if err != nil {
		return nil, fmt.Errorf("调用商品服务失败: %v", err)
	}

	if !deductResp.Success {
		fmt.Printf("❌ 库存不足，秒杀失败\n")
		return &pb.CreateOrderResponse{
			Success: false,
			Message: deductResp.Message,
		}, nil
	}

	// 2. 查价格、入库
	pResp, err := productClient.GetProduct(context.Background(), &pb.ProductRequest{ProductId: req.ProductId})
	if err != nil {
		return nil, err
	}

	totalAmount := pResp.Price * float32(req.Count)
	orderID := fmt.Sprintf("%d%d", time.Now().UnixNano(), rand.Intn(1000))

	order := Order{
		OrderID:   orderID,
		UserID:    req.UserId,
		ProductID: req.ProductId,
		Amount:    totalAmount,
		Status:    1,
	}

	if err := db.Create(&order).Error; err != nil {
		return nil, fmt.Errorf("创建订单失败: %v", err)
	}

	return &pb.CreateOrderResponse{
		OrderId: orderID,
		Success: true,
		Message: "抢购成功",
	}, nil
}

// === 初始化 Product Client ===
func initProductClient() {
	etcdAddr := config.Conf.Etcd.Addr

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdAddr},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}

	etcdResolver, err := resolver.NewBuilder(cli)
	if err != nil {
		log.Fatal(err)
	}

	conn, err := grpc.Dial(
		PRODUCT_SERVICE_NAME,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(etcdResolver),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)
	if err != nil {
		log.Fatal(err)
	}

	productClient = pb.NewProductServiceClient(conn)
	fmt.Println("✅ 已连接到商品服务 (RPC Client Ready)")
}

// === 注册自己到 Etcd ===
func registerEtcd() {
	etcdAddr := config.Conf.Etcd.Addr

	cli, _ := clientv3.New(clientv3.Config{Endpoints: []string{etcdAddr}})
	em, _ := endpoints.NewManager(cli, SERVICE_NAME)
	lease, _ := cli.Grant(context.TODO(), 10)
	em.AddEndpoint(context.TODO(), SERVICE_NAME+"/"+SERVICE_ADDR, endpoints.Endpoint{Addr: SERVICE_ADDR}, clientv3.WithLease(lease.ID))
	ch, _ := cli.KeepAlive(context.TODO(), lease.ID)
	go func() {
		for range ch {
		}
	}()
	fmt.Printf("✅ 订单服务已注册到 Etcd: %s\n", SERVICE_ADDR)
}

func initDB() {
	dsn := config.Conf.MySQL.DSN
	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	// 1. 最先加载配置
	config.InitConfig()

	initDB()
	initProductClient()
	registerEtcd()

	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatal(err)
	}

	s := grpc.NewServer()
	pb.RegisterOrderServiceServer(s, &server{})

	fmt.Println("=== 订单微服务已启动 (Port: 50052) ===")
	s.Serve(lis)
}
