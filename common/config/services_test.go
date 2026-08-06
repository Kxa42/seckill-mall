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
	if cfg.Catalog.ServiceName != "catalog-service" || cfg.Catalog.Address != "127.0.0.1:51002" {
		t.Fatalf("unexpected gateway catalog discovery config: %+v", cfg.Catalog)
	}
	if cfg.Inventory.ServiceName != "inventory-service" || cfg.Inventory.Address != "127.0.0.1:51003" {
		t.Fatalf("unexpected gateway inventory discovery config: %+v", cfg.Inventory)
	}
	if cfg.Commerce.URL != "http://127.0.0.1:8081" {
		t.Fatalf("unexpected gateway commerce URL: %q", cfg.Commerce.URL)
	}
	if cfg.MySQL.DSN != "" || cfg.MQ.URL != "" {
		t.Fatalf("gateway must not inherit business service credentials: mysql=%q mq=%q", cfg.MySQL.DSN, cfg.MQ.URL)
	}
}

func TestLoadServiceRuntimeConfigRequiresSelectedEnvironmentVariable(t *testing.T) {
	t.Setenv("CATALOG_RABBITMQ_URL", "amqp://catalog:secret@mq/")
	previous, existed := os.LookupEnv("CATALOG_MYSQL_DSN")
	_ = os.Unsetenv("CATALOG_MYSQL_DSN")
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv("CATALOG_MYSQL_DSN", previous)
		} else {
			_ = os.Unsetenv("CATALOG_MYSQL_DSN")
		}
	})
	path := writeServicesConfig(t)

	_, err := LoadServiceRuntimeConfig(path, "catalog")
	if err == nil {
		t.Fatal("LoadServiceRuntimeConfig() should reject missing selected environment variable")
	}
	if !strings.Contains(err.Error(), "CATALOG_MYSQL_DSN") {
		t.Fatalf("missing variable should be named in error: %v", err)
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
	t.Setenv("CATALOG_MYSQL_DSN", "mysql://catalog:secret@db/catalog")
	t.Setenv("CATALOG_RABBITMQ_URL", "amqp://catalog:secret@mq/")
	path := filepath.Join("..", "..", "config", "commerce-services.example.yaml")

	catalog, err := LoadServiceRuntimeConfig(path, "catalog")
	if err != nil {
		t.Fatalf("catalog template load error = %v", err)
	}
	if catalog.Catalog.ServiceName != "catalog-service" || catalog.Server.Port != "51002" {
		t.Fatalf("unexpected catalog template mapping: %+v %+v", catalog.Catalog, catalog.Server)
	}

	gateway, err := LoadServiceRuntimeConfig(path, "gateway")
	if err != nil {
		t.Fatalf("gateway template load error = %v", err)
	}
	if gateway.Catalog.ServiceName != "catalog-service" || gateway.Inventory.ServiceName != "inventory-service" {
		t.Fatalf("unexpected gateway template mapping: catalog=%+v inventory=%+v", gateway.Catalog, gateway.Inventory)
	}
}

func TestLoadServiceRuntimeConfigRejectsInvalidAddress(t *testing.T) {
	t.Setenv("CATALOG_MYSQL_DSN", "mysql://catalog:secret@db/catalog")
	t.Setenv("CATALOG_RABBITMQ_URL", "amqp://catalog:secret@mq/")
	path := writeServicesConfig(t)
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
  inventory:
    name: inventory-service
    address: 127.0.0.1:51003
    mysql_dsn: "${INVENTORY_MYSQL_DSN}"
    rabbitmq_url: "${INVENTORY_RABBITMQ_URL}"
    etcd_addr: 127.0.0.1:2379
    redis_addr: 127.0.0.1:6379
    redis_db: 2
    store: redis
    purchase_limit: 5
gateway:
  name: api-gateway
  http_address: 127.0.0.1:8080
  etcd_addr: 127.0.0.1:2379
  commerce_url: http://127.0.0.1:8081
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
