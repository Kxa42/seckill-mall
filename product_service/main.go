package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"seckill-mall/common/config"
	"strconv"

	"google.golang.org/grpc"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/endpoints"

	"seckill-mall/common/pb"

	// 引入 Redis 库
	"github.com/redis/go-redis/v9"
)

const (
	ETCD_ADDR    = "127.0.0.1:2379"
	SERVICE_NAME = "seckill/product"
	SERVICE_ADDR = "127.0.0.1:50051"
)

// === 1. 定义 Lua 脚本 (核心) ===
// KEYS[1]: 商品的 Redis Key (例如 product:stock:1)
// ARGV[1]: 要扣减的数量
const LUA_SCRIPT = `
local key = KEYS[1]
local change = tonumber(ARGV[1])

-- 获取当前库存
local stock = tonumber(redis.call('get', key))

-- 如果库存还没预热，直接返回错误
if not stock then
  return -1
end

-- 如果库存足够，就扣减
if stock >= change then
  redis.call('DECRBY', key, change)
  return 1 -- 成功
else
  return 0 -- 库存不足
end
`

// ... (Product 结构体, initRedis, initDB, preheatStock 等保持不变) ...

// === 2. 实现 DeductStock 接口 ===
func (s *server) DeductStock(ctx context.Context, req *pb.DeductStockRequest) (*pb.DeductStockResponse, error) {
	// 拼接 Key: product:stock:1
	key := "product:stock:" + strconv.FormatInt(req.ProductId, 10)

	// 执行 Lua 脚本
	// Eval(ctx, 脚本, Key列表, 参数列表)
	val, err := rdb.Eval(ctx, LUA_SCRIPT, []string{key}, req.Count).Int()

	if err != nil {
		return &pb.DeductStockResponse{Success: false, Message: "Redis 错误: " + err.Error()}, nil
	}

	if val == -1 {
		return &pb.DeductStockResponse{Success: false, Message: "商品未预热/不存在"}, nil
	}
	if val == 0 {
		return &pb.DeductStockResponse{Success: false, Message: "库存不足"}, nil
	}

	fmt.Printf("⚡ 秒杀成功！扣减 Redis 库存，商品: %d, 数量: %d\n", req.ProductId, req.Count)
	return &pb.DeductStockResponse{Success: true, Message: "扣减成功"}, nil
}

// === 数据库模型 ===
type Product struct {
	ID          int64   `gorm:"primaryKey"`
	Name        string  `gorm:"type:varchar(255)"`
	Price       float32 `gorm:"type:decimal(10,2)"`
	Stock       int32   `gorm:"type:int"`
	Description string  `gorm:"type:varchar(255)"`
}

func (Product) TableName() string { return "product" }

var db *gorm.DB
var rdb *redis.Client // 全局 Redis 客户端

type server struct {
	pb.UnimplementedProductServiceServer
}

// GetProduct 实现
func (s *server) GetProduct(ctx context.Context, req *pb.ProductRequest) (*pb.ProductResponse, error) {
	// ... 这里保持不变 ...
	var product Product
	if err := db.First(&product, req.ProductId).Error; err != nil {
		return nil, err
	}
	return &pb.ProductResponse{
		ProductId: product.ID, Name: product.Name, Price: product.Price,
	}, nil
}

// === 新增：初始化 Redis ===
func initRedis() {
	rdb = redis.NewClient(&redis.Options{
		Addr:     config.Conf.Redis.Addr,
		Password: config.Conf.Redis.Password,
		DB:       config.Conf.Redis.DB,
	})

	// 测试连接
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("连接 Redis 失败: %v", err)
	}
	fmt.Println("Redis 连接成功！")
}

// === 新增：库存预热 (把 MySQL 的库存同步到 Redis) ===
// 实际生产中，这个通常通过后台管理系统触发，这里我们简化为启动时自动加载
func preheatStock() {
	var products []Product
	db.Find(&products) // 查出所有商品

	for _, p := range products {
		key := "product:stock:" + strconv.FormatInt(p.ID, 10)

		// SetNX: 如果 Key 不存在才设置 (防止重启服务覆盖了已经扣减的库存)
		// 这里的 value 就是库存数 (例如 100)
		err := rdb.SetNX(context.Background(), key, p.Stock, 0).Err()
		if err != nil {
			fmt.Printf("预热库存失败 %d: %v\n", p.ID, err)
		} else {
			fmt.Printf("🔥 库存已预热: %s => %d\n", key, p.Stock)
		}
	}
}

// ... RegisterEtcd 和 initDB 函数保持不变 (请保留它们！) ...
// 为了篇幅，我这里简写了，请务必保留你原来的 RegisterEtcd 和 initDB 代码！

// 复制一份之前的 RegisterEtcd 和 initDB 放在这里...
func RegisterEtcd() {
	// ... 原样保留 ...
	cli, _ := clientv3.New(clientv3.Config{Endpoints: []string{config.Conf.Etcd.Addr}})
	em, _ := endpoints.NewManager(cli, SERVICE_NAME)
	lease, _ := cli.Grant(context.TODO(), 10)
	em.AddEndpoint(context.TODO(), SERVICE_NAME+"/"+SERVICE_ADDR, endpoints.Endpoint{Addr: SERVICE_ADDR}, clientv3.WithLease(lease.ID))
	ch, _ := cli.KeepAlive(context.TODO(), lease.ID)
	go func() {
		for range ch {
		}
	}()
	fmt.Printf("✅ 服务已注册到 Etcd\n")
}

func initDB() {
	dsn := config.Conf.MySQL.DSN
	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("MySQL 连接成功！")
}

func main() {
	config.InitConfig()
	initDB()
	initRedis()    // 1. 连 Redis
	preheatStock() // 2. 预热库存
	RegisterEtcd()

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal(err)
	}
	s := grpc.NewServer()
	pb.RegisterProductServiceServer(s, &server{})
	fmt.Println("=== 商品微服务 (Redis版) 已启动 ===")
	s.Serve(lis)
}
