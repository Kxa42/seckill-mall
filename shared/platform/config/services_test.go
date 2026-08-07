package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadServiceRuntimeConfigSelectsRoleAndExpandsSelectedSecrets(t *testing.T) {
	t.Setenv("CATALOG_MYSQL_DSN", "mysql://catalog:secret@db/catalog")
	t.Setenv("CATALOG_RABBITMQ_URL", "amqp://catalog:secret@mq/")
	path := writeServicesConfig(t)

	cfg, err := LoadServiceRuntimeConfig(path, "catalog")
	if err != nil {
		t.Fatalf("LoadServiceRuntimeConfig() error = %v", err)
	}
	if cfg.Server.Name != "catalog-service" || cfg.Server.Port != "51002" {
		t.Fatalf("unexpected catalog server config: %+v", cfg.Server)
	}
	if cfg.Server.MetricsPort != "9102" {
		t.Fatalf("unexpected catalog metrics port: %q", cfg.Server.MetricsPort)
	}
	if cfg.Catalog.ServiceName != "catalog-service" || cfg.Catalog.MySQLDSN != "mysql://catalog:secret@db/catalog" {
		t.Fatalf("unexpected catalog config: %+v", cfg.Catalog)
	}
	if cfg.MQ.URL != "amqp://catalog:secret@mq/" {
		t.Fatalf("unexpected catalog MQ URL: %q", cfg.MQ.URL)
	}
}

func TestLoadServiceRuntimeConfigMapsGatewayDiscoveryWithoutBusinessSecrets(t *testing.T) {
	path := writeServicesConfig(t)

	cfg, err := LoadServiceRuntimeConfig(path, "gateway")
	if err != nil {
		t.Fatalf("LoadServiceRuntimeConfig() error = %v", err)
	}
	if cfg.Server.Name != "api-gateway" || cfg.Server.Port != "8080" {
		t.Fatalf("unexpected gateway server config: %+v", cfg.Server)
	}
	if cfg.Server.Mode != "debug" {
		t.Fatalf("unexpected gateway mode: %q", cfg.Server.Mode)
	}
	if cfg.Server.MetricsPort != "9090" {
		t.Fatalf("unexpected gateway metrics port: %q", cfg.Server.MetricsPort)
	}
	if cfg.Catalog.ServiceName != "catalog-service" || cfg.Catalog.Address != "127.0.0.1:51002" {
		t.Fatalf("unexpected gateway catalog discovery config: %+v", cfg.Catalog)
	}
	if cfg.Inventory.ServiceName != "inventory-service" || cfg.Inventory.Address != "127.0.0.1:51003" {
		t.Fatalf("unexpected gateway inventory discovery config: %+v", cfg.Inventory)
	}
	if cfg.Identity.ServiceName != "identity-service" || cfg.Cart.ServiceName != "cart-service" || cfg.Order.ServiceName != "order-service" {
		t.Fatalf("unexpected gateway core discovery config: identity=%+v cart=%+v order=%+v", cfg.Identity, cfg.Cart, cfg.Order)
	}
	if cfg.Payment.ServiceName != "payment-service" || cfg.Fulfillment.ServiceName != "fulfillment-service" {
		t.Fatalf("unexpected gateway post-order discovery config: payment=%+v fulfillment=%+v", cfg.Payment, cfg.Fulfillment)
	}
	if cfg.MySQL.DSN != "" || cfg.MQ.URL != "" {
		t.Fatalf("gateway must not inherit business service credentials: mysql=%q mq=%q", cfg.MySQL.DSN, cfg.MQ.URL)
	}
	if cfg.JWT.Expire != "24h" {
		t.Fatalf("unexpected gateway jwt expire: %q", cfg.JWT.Expire)
	}
	if cfg.JWT.Secret != "" {
		// jwt.secret uses ${SECKILL_JWT_SECRET}; test env does not set it, so it must fall back to empty.
		t.Fatalf("gateway must fall back to empty jwt secret when env missing: %q", cfg.JWT.Secret)
	}
}

// TestLoadServiceRuntimeConfigFallsBackWhenOptionalCredentialsMissing 验证可选凭据
// (mysql_dsn/rabbitmq_url) 未设置环境变量时回退为空，catalog 仍能以内存仓储启动。
// 这修复了"manifest 路径破坏 catalog 内存模式"的回归。
func TestLoadServiceRuntimeConfigFallsBackWhenOptionalCredentialsMissing(t *testing.T) {
	previousDSN, dsnExisted := os.LookupEnv("CATALOG_MYSQL_DSN")
	previousMQ, mqExisted := os.LookupEnv("CATALOG_RABBITMQ_URL")
	_ = os.Unsetenv("CATALOG_MYSQL_DSN")
	_ = os.Unsetenv("CATALOG_RABBITMQ_URL")
	t.Cleanup(func() {
		if dsnExisted {
			_ = os.Setenv("CATALOG_MYSQL_DSN", previousDSN)
		} else {
			_ = os.Unsetenv("CATALOG_MYSQL_DSN")
		}
		if mqExisted {
			_ = os.Setenv("CATALOG_RABBITMQ_URL", previousMQ)
		} else {
			_ = os.Unsetenv("CATALOG_RABBITMQ_URL")
		}
	})
	path := writeServicesConfig(t)

	cfg, err := LoadServiceRuntimeConfig(path, "catalog")
	if err != nil {
		t.Fatalf("LoadServiceRuntimeConfig() should fall back when optional credentials are missing: %v", err)
	}
	if cfg.Catalog.MySQLDSN != "" {
		t.Fatalf("missing optional DSN must fall back to empty, got %q", cfg.Catalog.MySQLDSN)
	}
	if cfg.MQ.URL != "" {
		t.Fatalf("missing optional MQ URL must fall back to empty, got %q", cfg.MQ.URL)
	}
}

func TestLoadServiceRuntimeConfigRejectsMissingRequiredDiscoveryVariable(t *testing.T) {
	path := writeServicesConfig(t)
	// 把 catalog 的 etcd_addr 改成 ${VAR} 形式，必填变量缺失必须在启动前失败。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	catalogBlock := `  catalog:
    name: catalog-service
    address: 127.0.0.1:51002
    mysql_dsn: "${CATALOG_MYSQL_DSN}"
    rabbitmq_url: "${CATALOG_RABBITMQ_URL}"
    etcd_addr: 127.0.0.1:2379
    metrics_port: "9102"
`
	catalogReplacement := strings.Replace(catalogBlock, "etcd_addr: 127.0.0.1:2379", "etcd_addr: \"${CATALOG_ETCD_ADDR}\"", 1)
	data = []byte(strings.Replace(string(data), catalogBlock, catalogReplacement, 1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("rewrite fixture: %v", err)
	}
	_, err = LoadServiceRuntimeConfig(path, "catalog")
	if err == nil {
		t.Fatal("LoadServiceRuntimeConfig() should reject missing required discovery variable")
	}
	if !strings.Contains(err.Error(), "CATALOG_ETCD_ADDR") {
		t.Fatalf("missing required variable should be named in error: %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("configuration error must not expose secret values: %v", err)
	}
}

func TestLoadServiceRuntimeConfigRejectsUnknownRole(t *testing.T) {
	_, err := LoadServiceRuntimeConfig(writeServicesConfig(t), "unknown")
	if err == nil || !strings.Contains(err.Error(), `service "unknown" is missing`) {
		t.Fatalf("unexpected unknown role error: %v", err)
	}
}

func TestInitConfigUsesServiceManifestWhenConfigured(t *testing.T) {
	previous := Conf
	t.Cleanup(func() { Conf = previous })
	t.Setenv("SECKILL_SERVICES_CONFIG", writeServicesConfig(t))

	InitConfig("gateway")
	if Conf == nil || Conf.Server.Name != "api-gateway" {
		t.Fatalf("InitConfig() did not load gateway role: %+v", Conf)
	}
	if Conf.Catalog.ServiceName != "catalog-service" || Conf.Inventory.ServiceName != "inventory-service" {
		t.Fatalf("InitConfig() did not load downstream service names: catalog=%q inventory=%q", Conf.Catalog.ServiceName, Conf.Inventory.ServiceName)
	}
}

func TestLoadActualCommerceServicesTemplateByRole(t *testing.T) {
	path := filepath.Join("..", "..", "..", "deploy", "config", "commerce-services.example.yaml")

	// 不设置 CATALOG_MYSQL_DSN/RABBITMQ_URL，验证真实模板允许 catalog 以内存模式加载。
	catalog, err := LoadServiceRuntimeConfig(path, "catalog")
	if err != nil {
		t.Fatalf("catalog template load error = %v", err)
	}
	if catalog.Catalog.ServiceName != "catalog-service" || catalog.Server.Port != "51002" {
		t.Fatalf("unexpected catalog template mapping: %+v %+v", catalog.Catalog, catalog.Server)
	}
	if catalog.Catalog.MySQLDSN != "" || catalog.MQ.URL != "" {
		t.Fatalf("catalog optional credentials should fall back to empty without env: dsn=%q mq=%q", catalog.Catalog.MySQLDSN, catalog.MQ.URL)
	}
	if catalog.Server.MetricsPort != "9102" {
		t.Fatalf("catalog metrics port should map from template: %q", catalog.Server.MetricsPort)
	}

	gateway, err := LoadServiceRuntimeConfig(path, "gateway")
	if err != nil {
		t.Fatalf("gateway template load error = %v", err)
	}
	if gateway.Catalog.ServiceName != "catalog-service" || gateway.Inventory.ServiceName != "inventory-service" {
		t.Fatalf("unexpected gateway template mapping: catalog=%+v inventory=%+v", gateway.Catalog, gateway.Inventory)
	}
	if gateway.Order.ServiceName != "order-service" || gateway.Order.Address != "127.0.0.1:51005" {
		t.Fatalf("unexpected gateway order mapping: %+v", gateway.Order)
	}
	if gateway.Server.Mode != "debug" || gateway.Server.MetricsPort != "9090" {
		t.Fatalf("gateway mode/metrics should map from template: mode=%q metrics=%q", gateway.Server.Mode, gateway.Server.MetricsPort)
	}
}

func TestLoadServiceRuntimeConfigMapsOrderDependencies(t *testing.T) {
	path := writeServicesConfig(t)
	cfg, err := LoadServiceRuntimeConfig(path, "order")
	if err != nil {
		t.Fatalf("order config load error = %v", err)
	}
	if cfg.Order.ServiceName != "order-service" || cfg.Server.Port != "51005" {
		t.Fatalf("unexpected order config: %+v %+v", cfg.Order, cfg.Server)
	}
	if cfg.Catalog.Address != "127.0.0.1:51002" || cfg.Identity.Address != "127.0.0.1:51001" || cfg.Inventory.Address != "127.0.0.1:51003" {
		t.Fatalf("order dependencies not mapped: catalog=%+v identity=%+v inventory=%+v", cfg.Catalog, cfg.Identity, cfg.Inventory)
	}
}

func TestLoadServiceRuntimeConfigMapsStage4Dependencies(t *testing.T) {
	path := writeServicesConfig(t)
	t.Setenv("PAYMENT_SECRET", "payment-secret-value-32-characters")

	cart, err := LoadServiceRuntimeConfig(path, "cart")
	if err != nil {
		t.Fatalf("cart config load error = %v", err)
	}
	if cart.Cart.ServiceName != "cart-service" || cart.Server.Port != "51004" || cart.Catalog.Address != "127.0.0.1:51002" {
		t.Fatalf("unexpected cart config: cart=%+v catalog=%+v server=%+v", cart.Cart, cart.Catalog, cart.Server)
	}

	for _, role := range []string{"payment", "fulfillment"} {
		cfg, loadErr := LoadServiceRuntimeConfig(path, role)
		if loadErr != nil {
			t.Fatalf("%s config load error = %v", role, loadErr)
		}
		if cfg.Order.ServiceName != "order-service" || cfg.Order.Address != "127.0.0.1:51005" {
			t.Fatalf("%s order dependency not mapped: %+v", role, cfg.Order)
		}
		if role == "payment" && cfg.Payment.Secret != "payment-secret-value-32-characters" {
			t.Fatalf("payment secret was not expanded: %q", cfg.Payment.Secret)
		}
	}
}

// TestLoadServiceRuntimeConfigRejectsRedisStoreWithoutRedisAddr 验证 inventory 在
// store=redis 但 redis_addr 缺失时于加载阶段失败，而非推迟到运行时 redis 连接崩溃。
func TestLoadServiceRuntimeConfigRejectsRedisStoreWithoutRedisAddr(t *testing.T) {
	path := writeServicesConfig(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	data = []byte(strings.Replace(string(data), "    redis_addr: 127.0.0.1:6379\n", "", 1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("rewrite fixture: %v", err)
	}
	_, err = LoadServiceRuntimeConfig(path, "inventory")
	if err == nil || !strings.Contains(err.Error(), "redis_addr is required") {
		t.Fatalf("inventory with store=redis and missing redis_addr should be rejected: %v", err)
	}
}

func TestLoadServiceRuntimeConfigRejectsMissingEtcdAddr(t *testing.T) {
	path := writeServicesConfig(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// 移除 catalog 的 etcd_addr 行，必填字段缺失必须失败。
	catalogBlock := "  catalog:\n    name: catalog-service\n    address: 127.0.0.1:51002\n    mysql_dsn: \"${CATALOG_MYSQL_DSN}\"\n    rabbitmq_url: \"${CATALOG_RABBITMQ_URL}\"\n    etcd_addr: 127.0.0.1:2379\n    metrics_port: \"9102\"\n"
	missingBlock := "  catalog:\n    name: catalog-service\n    address: 127.0.0.1:51002\n    mysql_dsn: \"${CATALOG_MYSQL_DSN}\"\n    rabbitmq_url: \"${CATALOG_RABBITMQ_URL}\"\n    metrics_port: \"9102\"\n"
	data = []byte(strings.Replace(string(data), catalogBlock, missingBlock, 1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("rewrite fixture: %v", err)
	}
	_, err = LoadServiceRuntimeConfig(path, "catalog")
	if err == nil || !strings.Contains(err.Error(), "etcd_addr is required") {
		t.Fatalf("catalog missing etcd_addr should be rejected: %v", err)
	}
}

func TestLoadServiceRuntimeConfigRejectsInvalidAddress(t *testing.T) {
	path := writeServicesConfig(t) // 必填字段齐全；可选凭据缺失回退空即可。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	data = []byte(strings.Replace(string(data), "127.0.0.1:51002", "catalog-without-port", 1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("rewrite fixture: %v", err)
	}

	_, err = LoadServiceRuntimeConfig(path, "catalog")
	if err == nil || !strings.Contains(err.Error(), "address is invalid") {
		t.Fatalf("unexpected invalid address error: %v", err)
	}
}

func writeServicesConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "commerce-services.yaml")
	content := `services:
  identity:
    name: identity-service
    address: 127.0.0.1:51001
    mysql_dsn: "${IDENTITY_MYSQL_DSN}"
    rabbitmq_url: "${IDENTITY_RABBITMQ_URL}"
    etcd_addr: 127.0.0.1:2379
  catalog:
    name: catalog-service
    address: 127.0.0.1:51002
    mysql_dsn: "${CATALOG_MYSQL_DSN}"
    rabbitmq_url: "${CATALOG_RABBITMQ_URL}"
    etcd_addr: 127.0.0.1:2379
    metrics_port: "9102"
  inventory:
    name: inventory-service
    address: 127.0.0.1:51003
    mysql_dsn: "${INVENTORY_MYSQL_DSN}"
    rabbitmq_url: "${INVENTORY_RABBITMQ_URL}"
    etcd_addr: 127.0.0.1:2379
    redis_addr: 127.0.0.1:6379
    redis_password: "${INVENTORY_REDIS_PASSWORD}"
    redis_db: 2
    store: redis
    purchase_limit: 5
    metrics_port: "9103"
  cart:
    name: cart-service
    address: 127.0.0.1:51004
    mysql_dsn: "${CART_MYSQL_DSN}"
    rabbitmq_url: "${CART_RABBITMQ_URL}"
    etcd_addr: 127.0.0.1:2379
  order:
    name: order-service
    address: 127.0.0.1:51005
    mysql_dsn: "${ORDER_MYSQL_DSN}"
    rabbitmq_url: "${ORDER_RABBITMQ_URL}"
    etcd_addr: 127.0.0.1:2379
  payment:
    name: payment-service
    address: 127.0.0.1:51006
    mysql_dsn: "${PAYMENT_MYSQL_DSN}"
    rabbitmq_url: "${PAYMENT_RABBITMQ_URL}"
    etcd_addr: 127.0.0.1:2379
    secret: "${PAYMENT_SECRET}"
  fulfillment:
    name: fulfillment-service
    address: 127.0.0.1:51007
    mysql_dsn: "${FULFILLMENT_MYSQL_DSN}"
    rabbitmq_url: "${FULFILLMENT_RABBITMQ_URL}"
    etcd_addr: 127.0.0.1:2379
gateway:
  name: api-gateway
  http_address: 127.0.0.1:8080
  etcd_addr: 127.0.0.1:2379
  metrics_port: "9090"
  mode: "debug"
  jwt:
    expire: "24h"
    secret: "${SECKILL_JWT_SECRET}"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
