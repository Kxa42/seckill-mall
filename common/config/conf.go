package config

import (
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	MySQL   MySQLConfig   `mapstructure:"mysql"`
	Redis   RedisConfig   `mapstructure:"redis"`
	Etcd    EtcdConfig    `mapstructure:"etcd"`
	Seckill SeckillConfig `mapstructure:"seckill"`
}

type ServerConfig struct {
	Name string `mapstructure:"name"`
	Mode string `mapstructure:"mode"`
}

type MySQLConfig struct {
	DSN string `mapstructure:"dsn"`
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

// 全局配置变量
var Conf *Config

// InitConfig 读取配置文件
func InitConfig() {
	viper.SetConfigName("config")         // 文件名 (不带后缀)
	viper.SetConfigType("yaml")           // 文件格式
	viper.AddConfigPath(".")              // 搜索路径 (当前根目录)
	viper.AddConfigPath("./seckill-mall") // 防止在子目录下运行找不到

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("读取配置文件失败: %v", err)
	}

	// 将读取的配置映射到结构体中
	if err := viper.Unmarshal(&Conf); err != nil {
		log.Fatalf("解析配置文件失败: %v", err)
	}

	log.Println("配置加载成功！")
}
