// Package config 负责商城后端进程的显式配置加载与校验。
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const minimumSecretLength = 32

// Commerce 描述 commerce-api 的运行配置。
type Commerce struct {
	HTTPAddr          string
	StoreDriver       string
	MySQLDSN          string
	JWTSecret         string
	MockPaymentSecret string
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	OrderTTL          time.Duration
	AdminEmail        string
	AdminPassword     string
}

// LoadCommerce 从环境变量加载并校验商城 API 配置。
func LoadCommerce() (Commerce, error) {
	cfg := Commerce{
		HTTPAddr:          envOrDefault("SECKILL_COMMERCE_HTTP_ADDR", ":8081"),
		StoreDriver:       strings.ToLower(envOrDefault("SECKILL_COMMERCE_STORE", "mysql")),
		MySQLDSN:          strings.TrimSpace(os.Getenv("SECKILL_MYSQL_DSN")),
		JWTSecret:         strings.TrimSpace(os.Getenv("SECKILL_JWT_SECRET")),
		MockPaymentSecret: strings.TrimSpace(os.Getenv("SECKILL_MOCK_PAYMENT_SECRET")),
		AdminEmail:        strings.TrimSpace(os.Getenv("SECKILL_ADMIN_EMAIL")),
		AdminPassword:     os.Getenv("SECKILL_ADMIN_PASSWORD"),
	}

	var err error
	if cfg.AccessTokenTTL, err = durationEnv("SECKILL_ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		return Commerce{}, err
	}
	if cfg.RefreshTokenTTL, err = durationEnv("SECKILL_REFRESH_TOKEN_TTL", 7*24*time.Hour); err != nil {
		return Commerce{}, err
	}
	if cfg.OrderTTL, err = durationEnv("SECKILL_ORDER_TTL", 15*time.Minute); err != nil {
		return Commerce{}, err
	}

	if cfg.StoreDriver != "mysql" && cfg.StoreDriver != "memory" {
		return Commerce{}, fmt.Errorf("SECKILL_COMMERCE_STORE 只支持 mysql 或 memory")
	}
	if cfg.StoreDriver == "mysql" && cfg.MySQLDSN == "" {
		return Commerce{}, fmt.Errorf("SECKILL_MYSQL_DSN 不能为空")
	}
	if len(cfg.JWTSecret) < minimumSecretLength {
		return Commerce{}, fmt.Errorf("SECKILL_JWT_SECRET 长度不能小于 %d", minimumSecretLength)
	}
	if len(cfg.MockPaymentSecret) < minimumSecretLength {
		return Commerce{}, fmt.Errorf("SECKILL_MOCK_PAYMENT_SECRET 长度不能小于 %d", minimumSecretLength)
	}
	if (cfg.AdminEmail == "") != (cfg.AdminPassword == "") {
		return Commerce{}, fmt.Errorf("SECKILL_ADMIN_EMAIL 和 SECKILL_ADMIN_PASSWORD 必须同时设置")
	}
	if cfg.AdminPassword != "" && len(cfg.AdminPassword) < 12 {
		return Commerce{}, fmt.Errorf("SECKILL_ADMIN_PASSWORD 长度不能小于 12")
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s 必须是正数时间长度: %q", key, value)
	}
	return duration, nil
}
