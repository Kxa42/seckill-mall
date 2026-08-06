package config

import (
	"log"
	"os"
	"strconv"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	MySQL     MySQLConfig     `mapstructure:"mysql"`
	MQ        MQConfig        `mapstructure:"mq"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Etcd      EtcdConfig      `mapstructure:"etcd"`
	Seckill   SeckillConfig   `mapstructure:"seckill"`
	JWT       JWTConfig       `mapstructure:"jwt"`
	Commerce  CommerceConfig  `mapstructure:"commerce"`
	Catalog   CatalogConfig   `mapstructure:"catalog"`
	Inventory InventoryConfig `mapstructure:"inventory"`
}

type ServerConfig struct {
	Name        string `mapstructure:"name" yaml:"name"`
	Mode        string `mapstructure:"mode"`
	Port        string `mapstructure:"port" yaml:"port"`
	MetricsPort string `mapstructure:"metrics_port"`
}

type MySQLConfig struct {
	DSN string `mapstructure:"dsn"`
}

type MQConfig struct {
	URL string `mapstructure:"url"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type EtcdConfig struct {
	Addr string `mapstructure:"addr"`
}

type SeckillConfig struct {
	PurchaseLimit int64 `mapstructure:"purchase_limit"`
}

type JWTConfig struct {
	Expire string `mapstructure:"expire"` //对应 yaml 里的 "24h"
	Secret string `mapstructure:"secret"`
}

type CommerceConfig struct {
	URL string `mapstructure:"url"`
}

// CatalogConfig 描述 Catalog 服务自身或 Gateway 的服务发现目标。
type CatalogConfig struct {
	ServiceName string `mapstructure:"service_name"`
	Address     string `mapstructure:"address"`
	MySQLDSN    string `mapstructure:"mysql_dsn"`
}

// InventoryConfig 描述 Inventory/Seckill 服务自身或 Gateway 的服务发现目标。
type InventoryConfig struct {
	ServiceName   string `mapstructure:"service_name"`
	Address       string `mapstructure:"address"`
	Store         string `mapstructure:"store"`
	RedisAddr     string `mapstructure:"redis_addr"`
	RedisPassword string `mapstructure:"redis_password"`
	RedisDB       int    `mapstructure:"redis_db"`
	PurchaseLimit int32  `mapstructure:"purchase_limit"`
}

// 全局配置变量
var Conf *Config

// InitConfig 读取配置文件
func InitConfig(filename string) {
	viper.AddConfigPath("./config")       // 配置文件夹路径
	viper.AddConfigPath(".")              // 搜索当前根目录
	viper.AddConfigPath("./seckill-mall") // 防止在子目录下运行找不到

	viper.SetConfigName(filename) // 动态文件名
	viper.SetConfigType("yaml")   // 文件格式

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("config read failed: %v", err)
	}

	// 将读取的配置映射到结构体中
	if err := viper.Unmarshal(&Conf); err != nil {
		log.Fatalf("config unmarshal failed: %v", err)
	}

	applyEnvOverrides()

	log.Println("config loaded")
}

func applyEnvOverrides() {
	if dsn := os.Getenv("SECKILL_MYSQL_DSN"); dsn != "" {
		Conf.MySQL.DSN = dsn
	}

	if secret := os.Getenv("SECKILL_JWT_SECRET"); secret != "" {
		Conf.JWT.Secret = secret
	}

	if mqURL := os.Getenv("SECKILL_MQ_URL"); mqURL != "" {
		Conf.MQ.URL = mqURL
	}

	if commerceURL := os.Getenv("SECKILL_COMMERCE_URL"); commerceURL != "" {
		Conf.Commerce.URL = commerceURL
	}
	if serviceName := os.Getenv("SECKILL_CATALOG_SERVICE"); serviceName != "" {
		Conf.Catalog.ServiceName = serviceName
	}
	if serviceAddr := os.Getenv("SECKILL_CATALOG_ADDR"); serviceAddr != "" {
		Conf.Catalog.Address = serviceAddr
	}
	if catalogDSN := os.Getenv("SECKILL_CATALOG_MYSQL_DSN"); catalogDSN != "" {
		Conf.Catalog.MySQLDSN = catalogDSN
	}
	if serviceName := os.Getenv("SECKILL_INVENTORY_SERVICE"); serviceName != "" {
		Conf.Inventory.ServiceName = serviceName
	}
	if serviceAddr := os.Getenv("SECKILL_INVENTORY_ADDR"); serviceAddr != "" {
		Conf.Inventory.Address = serviceAddr
	}
	if store := os.Getenv("SECKILL_INVENTORY_STORE"); store != "" {
		Conf.Inventory.Store = store
	}
	if redisAddr := os.Getenv("SECKILL_INVENTORY_REDIS_ADDR"); redisAddr != "" {
		Conf.Inventory.RedisAddr = redisAddr
	}
	if redisPassword := os.Getenv("SECKILL_INVENTORY_REDIS_PASSWORD"); redisPassword != "" {
		Conf.Inventory.RedisPassword = redisPassword
	}
	if redisDB := os.Getenv("SECKILL_INVENTORY_REDIS_DB"); redisDB != "" {
		if parsed, err := strconv.Atoi(redisDB); err == nil && parsed >= 0 {
			Conf.Inventory.RedisDB = parsed
		}
	}
	if purchaseLimit := os.Getenv("SECKILL_INVENTORY_PURCHASE_LIMIT"); purchaseLimit != "" {
		if parsed, err := strconv.ParseInt(purchaseLimit, 10, 32); err == nil && parsed > 0 {
			Conf.Inventory.PurchaseLimit = int32(parsed)
		}
	}
	if etcdAddr := os.Getenv("SECKILL_ETCD_ADDR"); etcdAddr != "" {
		Conf.Etcd.Addr = etcdAddr
	}
	if redisAddr := os.Getenv("SECKILL_REDIS_ADDR"); redisAddr != "" {
		Conf.Redis.Addr = redisAddr
	}
	if redisPassword := os.Getenv("SECKILL_REDIS_PASSWORD"); redisPassword != "" {
		Conf.Redis.Password = redisPassword
	}
	if serverMode := os.Getenv("SECKILL_SERVER_MODE"); serverMode != "" {
		Conf.Server.Mode = serverMode
	}
}

// AdvertiseAddr 返回服务注册使用的地址，容器和多机部署可通过环境变量覆盖。
func AdvertiseAddr(fallback string) string {
	if value := os.Getenv("SECKILL_ADVERTISE_ADDR"); value != "" {
		return value
	}
	return fallback
}
