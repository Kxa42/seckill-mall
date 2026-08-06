package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadCommerce(t *testing.T) {
	t.Setenv("SECKILL_MYSQL_DSN", "user:pass@tcp(mysql:3306)/seckill")
	t.Setenv("SECKILL_JWT_SECRET", strings.Repeat("j", minimumSecretLength))
	t.Setenv("SECKILL_MOCK_PAYMENT_SECRET", strings.Repeat("p", minimumSecretLength))
	t.Setenv("SECKILL_ACCESS_TOKEN_TTL", "30m")

	cfg, err := LoadCommerce()
	if err != nil {
		t.Fatalf("LoadCommerce() error = %v", err)
	}
	if cfg.AccessTokenTTL != 30*time.Minute {
		t.Fatalf("AccessTokenTTL = %s", cfg.AccessTokenTTL)
	}
	if cfg.HTTPAddr != ":8081" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
}

func TestLoadCommerceRejectsWeakSecret(t *testing.T) {
	t.Setenv("SECKILL_MYSQL_DSN", "dsn")
	t.Setenv("SECKILL_JWT_SECRET", "short")
	t.Setenv("SECKILL_MOCK_PAYMENT_SECRET", strings.Repeat("p", minimumSecretLength))

	if _, err := LoadCommerce(); err == nil {
		t.Fatal("LoadCommerce() expected weak secret error")
	}
}

func TestLoadCommerceMemoryStoreDoesNotRequireDSN(t *testing.T) {
	t.Setenv("SECKILL_COMMERCE_STORE", "memory")
	t.Setenv("SECKILL_MYSQL_DSN", "")
	t.Setenv("SECKILL_JWT_SECRET", strings.Repeat("j", minimumSecretLength))
	t.Setenv("SECKILL_MOCK_PAYMENT_SECRET", strings.Repeat("p", minimumSecretLength))

	cfg, err := LoadCommerce()
	if err != nil {
		t.Fatalf("LoadCommerce() error = %v", err)
	}
	if cfg.StoreDriver != "memory" {
		t.Fatalf("StoreDriver = %q", cfg.StoreDriver)
	}
}
