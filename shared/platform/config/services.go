// Package config 提供统一商城服务配置模板的按角色加载能力。
// 统一模板只在显式指定时启用，服务仍可回退到现有独立 YAML 配置。
package config

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/spf13/viper"
)

// ServiceDefinition 描述目标商城业务服务的运行时连接配置。
type ServiceDefinition struct {
	Name          string `mapstructure:"name"`
	Address       string `mapstructure:"address"`
	MySQLDSN      string `mapstructure:"mysql_dsn"`
	RabbitMQURL   string `mapstructure:"rabbitmq_url"`
	EtcdAddr      string `mapstructure:"etcd_addr"`
	RedisAddr     string `mapstructure:"redis_addr"`
	RedisPassword string `mapstructure:"redis_password"`
	RedisDB       int    `mapstructure:"redis_db"`
	Store         string `mapstructure:"store"`
	Secret        string `mapstructure:"secret"`
	PurchaseLimit int32  `mapstructure:"purchase_limit"`
	// MetricsPort 为可选；留空则不启动独立 metrics server。
	MetricsPort string `mapstructure:"metrics_port"`
}

// GatewayDefinition 描述统一配置模板中的 Gateway 运行配置。
type GatewayDefinition struct {
	Name        string `mapstructure:"name"`
	HTTPAddress string `mapstructure:"http_address"`
	EtcdAddr    string `mapstructure:"etcd_addr"`
	// MetricsPort 为可选；留空则不启动独立 metrics server。
	MetricsPort string `mapstructure:"metrics_port"`
	// Mode 控制本地与 release 启动校验；留空按 release 处理。
	Mode string `mapstructure:"mode"`
	// JWT 为可选；Secret 用于校验 Identity Service 签发的令牌。
	JWT GatewayJWTDefinition `mapstructure:"jwt"`
}

// GatewayJWTDefinition 描述 Gateway 校验访问令牌所需的配置。
type GatewayJWTDefinition struct {
	Secret string `mapstructure:"secret"`
}

type serviceManifest struct {
	Services map[string]ServiceDefinition `mapstructure:"services"`
	Gateway  GatewayDefinition            `mapstructure:"gateway"`
}

// LoadServiceRuntimeConfig 从统一商城配置模板加载指定角色。
// 必填字段（name/address/etcd_addr）缺失或引用的环境变量缺失会在启动前失败；
// 可选凭据（mysql_dsn/rabbitmq_url/redis_password）缺失则回退为空，保持服务可在
// 无 Docker / 无外部位点环境以内存或本地模式启动。错误信息不包含任何变量值。
func LoadServiceRuntimeConfig(path, role string) (*Config, error) {
	manifest, err := readServiceManifest(path)
	if err != nil {
		return nil, err
	}
	role = strings.TrimSpace(role)
	if role == "" {
		return nil, fmt.Errorf("service role is required")
	}

	cfg := &Config{Server: ServerConfig{Mode: "release"}}
	if role == "gateway" {
		if err := expandGatewayDefinition(&manifest.Gateway); err != nil {
			return nil, err
		}
		if err := validateGatewayDefinition(manifest.Gateway); err != nil {
			return nil, err
		}
		port, err := portFromAddress(manifest.Gateway.HTTPAddress)
		if err != nil {
			return nil, fmt.Errorf("gateway http_address is invalid")
		}
		cfg.Server = ServerConfig{
			Name:        manifest.Gateway.Name,
			Mode:        gatewayModeOrDefault(manifest.Gateway.Mode),
			Port:        port,
			MetricsPort: strings.TrimSpace(manifest.Gateway.MetricsPort),
		}
		cfg.Etcd.Addr = manifest.Gateway.EtcdAddr
		cfg.JWT.Secret = strings.TrimSpace(manifest.Gateway.JWT.Secret)
		for _, serviceRole := range []string{"identity", "catalog", "inventory", "cart", "order", "payment", "fulfillment"} {
			definition, ok := manifest.Services[serviceRole]
			if !ok {
				return nil, fmt.Errorf("service %q is missing from configuration", serviceRole)
			}
			if err := expandDiscoveryDefinition(&definition); err != nil {
				return nil, fmt.Errorf("service %q configuration: %w", serviceRole, err)
			}
			if err := validateDiscoveryDefinition(serviceRole, definition); err != nil {
				return nil, err
			}
			if serviceRole == "catalog" {
				cfg.Catalog.ServiceName = definition.Name
				cfg.Catalog.Address = definition.Address
			} else if serviceRole == "inventory" {
				cfg.Inventory.ServiceName = definition.Name
				cfg.Inventory.Address = definition.Address
			} else if serviceRole == "identity" {
				cfg.Identity.ServiceName = definition.Name
				cfg.Identity.Address = definition.Address
			} else if serviceRole == "cart" {
				cfg.Cart.ServiceName = definition.Name
				cfg.Cart.Address = definition.Address
			} else if serviceRole == "payment" {
				cfg.Payment.ServiceName = definition.Name
				cfg.Payment.Address = definition.Address
			} else if serviceRole == "fulfillment" {
				cfg.Fulfillment.ServiceName = definition.Name
				cfg.Fulfillment.Address = definition.Address
			} else {
				cfg.Order.ServiceName = definition.Name
				cfg.Order.Address = definition.Address
			}
		}
		return cfg, nil
	}

	definition, ok := manifest.Services[role]
	if !ok {
		return nil, fmt.Errorf("service %q is missing from configuration", role)
	}
	if err := expandServiceDefinition(&definition); err != nil {
		return nil, fmt.Errorf("service %q configuration: %w", role, err)
	}
	if err := validateServiceDefinition(role, definition); err != nil {
		return nil, err
	}
	cfg.Server = ServerConfig{
		Name:        definition.Name,
		Mode:        serverModeOrDefault(definition.Store),
		Port:        "",
		MetricsPort: strings.TrimSpace(definition.MetricsPort),
	}
	cfg.Server.Port, err = portFromAddress(definition.Address)
	if err != nil {
		return nil, fmt.Errorf("service %q address is invalid", role)
	}
	cfg.MySQL.DSN = definition.MySQLDSN
	cfg.MQ.URL = definition.RabbitMQURL
	cfg.Etcd.Addr = definition.EtcdAddr
	cfg.Redis = RedisConfig{Addr: definition.RedisAddr, Password: definition.RedisPassword, DB: definition.RedisDB}

	switch role {
	case "identity":
		cfg.Identity = IdentityConfig{ServiceName: definition.Name, Address: definition.Address, MySQLDSN: definition.MySQLDSN}
	case "catalog":
		cfg.Catalog = CatalogConfig{ServiceName: definition.Name, Address: definition.Address, MySQLDSN: definition.MySQLDSN}
	case "inventory":
		cfg.Inventory = InventoryConfig{
			ServiceName: definition.Name, Address: definition.Address, Store: definition.Store,
			RedisAddr: definition.RedisAddr, RedisPassword: definition.RedisPassword,
			RedisDB: definition.RedisDB, PurchaseLimit: definition.PurchaseLimit,
		}
		cfg.Seckill.PurchaseLimit = int64(definition.PurchaseLimit)
	case "order":
		cfg.Order = OrderConfig{ServiceName: definition.Name, Address: definition.Address, MySQLDSN: definition.MySQLDSN}
		for _, dependency := range []string{"catalog", "identity", "inventory"} {
			dependencyDefinition, exists := manifest.Services[dependency]
			if !exists {
				return nil, fmt.Errorf("service %q is missing from configuration", dependency)
			}
			if err := expandDiscoveryDefinition(&dependencyDefinition); err != nil {
				return nil, fmt.Errorf("service %q configuration: %w", dependency, err)
			}
			if err := validateDiscoveryDefinition(dependency, dependencyDefinition); err != nil {
				return nil, err
			}
			switch dependency {
			case "catalog":
				cfg.Catalog = CatalogConfig{ServiceName: dependencyDefinition.Name, Address: dependencyDefinition.Address}
			case "identity":
				cfg.Identity = IdentityConfig{ServiceName: dependencyDefinition.Name, Address: dependencyDefinition.Address}
			case "inventory":
				cfg.Inventory = InventoryConfig{ServiceName: dependencyDefinition.Name, Address: dependencyDefinition.Address}
			}
		}
	case "cart":
		cfg.Cart = CartConfig{ServiceName: definition.Name, Address: definition.Address, MySQLDSN: definition.MySQLDSN}
		dependencyDefinition, exists := manifest.Services["catalog"]
		if !exists {
			return nil, fmt.Errorf("service %q is missing from configuration", "catalog")
		}
		if err := expandDiscoveryDefinition(&dependencyDefinition); err != nil {
			return nil, fmt.Errorf("service %q configuration: %w", "catalog", err)
		}
		if err := validateDiscoveryDefinition("catalog", dependencyDefinition); err != nil {
			return nil, err
		}
		cfg.Catalog = CatalogConfig{ServiceName: dependencyDefinition.Name, Address: dependencyDefinition.Address}
	case "payment":
		cfg.Payment = PaymentConfig{ServiceName: definition.Name, Address: definition.Address, MySQLDSN: definition.MySQLDSN, Secret: definition.Secret}
		dependencyDefinition, exists := manifest.Services["order"]
		if !exists {
			return nil, fmt.Errorf("service %q is missing from configuration", "order")
		}
		if err := expandDiscoveryDefinition(&dependencyDefinition); err != nil {
			return nil, fmt.Errorf("service %q configuration: %w", "order", err)
		}
		if err := validateDiscoveryDefinition("order", dependencyDefinition); err != nil {
			return nil, err
		}
		cfg.Order = OrderConfig{ServiceName: dependencyDefinition.Name, Address: dependencyDefinition.Address}
	case "fulfillment":
		cfg.Fulfillment = FulfillmentConfig{ServiceName: definition.Name, Address: definition.Address, MySQLDSN: definition.MySQLDSN}
		dependencyDefinition, exists := manifest.Services["order"]
		if !exists {
			return nil, fmt.Errorf("service %q is missing from configuration", "order")
		}
		if err := expandDiscoveryDefinition(&dependencyDefinition); err != nil {
			return nil, fmt.Errorf("service %q configuration: %w", "order", err)
		}
		if err := validateDiscoveryDefinition("order", dependencyDefinition); err != nil {
			return nil, err
		}
		cfg.Order = OrderConfig{ServiceName: dependencyDefinition.Name, Address: dependencyDefinition.Address}
	}
	return cfg, nil
}

func readServiceManifest(path string) (serviceManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return serviceManifest{}, fmt.Errorf("read services config: %w", err)
	}
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return serviceManifest{}, fmt.Errorf("parse services config: %w", err)
	}
	var manifest serviceManifest
	if err := v.Unmarshal(&manifest); err != nil {
		return serviceManifest{}, fmt.Errorf("decode services config: %w", err)
	}
	return manifest, nil
}

// expandServiceDefinition 展开必填的发现字段和可选的业务凭据。
// 可选凭据字段即使 ${VAR} 变量缺失也回退为空值，避免强制要求无 Docker 环境必须配置外部依赖。
func expandServiceDefinition(definition *ServiceDefinition) error {
	if err := expandDiscoveryDefinition(definition); err != nil {
		return err
	}
	optionalFields := map[string]*string{
		"mysql_dsn":      &definition.MySQLDSN,
		"rabbitmq_url":   &definition.RabbitMQURL,
		"redis_addr":     &definition.RedisAddr,
		"redis_password": &definition.RedisPassword,
		"secret":         &definition.Secret,
	}
	for name, value := range optionalFields {
		expanded, err := expandOptionalEnv(value)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		*value = expanded
	}
	return nil
}

func expandDiscoveryDefinition(definition *ServiceDefinition) error {
	for name, value := range map[string]*string{
		"name":      &definition.Name,
		"address":   &definition.Address,
		"etcd_addr": &definition.EtcdAddr,
	} {
		expanded, err := expandEnv(value)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		*value = expanded
	}
	if err := expandNamedField("metrics_port", &definition.MetricsPort); err != nil {
		return err
	}
	return nil
}

func expandGatewayDefinition(definition *GatewayDefinition) error {
	for name, value := range map[string]*string{
		"name":         &definition.Name,
		"http_address": &definition.HTTPAddress,
		"etcd_addr":    &definition.EtcdAddr,
	} {
		expanded, err := expandEnv(value)
		if err != nil {
			return fmt.Errorf("gateway %s: %w", name, err)
		}
		*value = expanded
	}
	// gateway metrics_port 与 JWT 校验密钥均为可选。
	if err := expandOptionalField("gateway metrics_port", &definition.MetricsPort); err != nil {
		return err
	}
	if err := expandOptionalField("gateway jwt.secret", &definition.JWT.Secret); err != nil {
		return err
	}
	return nil
}

// validateGatewayDefinition 校验 Gateway 必填字段并验证服务发现目标存在。
func validateGatewayDefinition(gateway GatewayDefinition) error {
	if strings.TrimSpace(gateway.Name) == "" {
		return fmt.Errorf("gateway name is required")
	}
	if strings.TrimSpace(gateway.HTTPAddress) == "" {
		return fmt.Errorf("gateway http_address is required")
	}
	if strings.TrimSpace(gateway.EtcdAddr) == "" {
		return fmt.Errorf("gateway etcd_addr is required")
	}
	return nil
}

// validateServiceDefinition 校验业务服务必填字段，并对 inventory 的 redis store 做必填校验。
func validateServiceDefinition(role string, definition ServiceDefinition) error {
	if err := validateDiscoveryDefinition(role, definition); err != nil {
		return err
	}
	if role == "inventory" && strings.EqualFold(strings.TrimSpace(definition.Store), "redis") {
		if strings.TrimSpace(definition.RedisAddr) == "" {
			return fmt.Errorf("service %q redis_addr is required when store is redis", role)
		}
	}
	return nil
}

// validateDiscoveryDefinition 校验服务发现必填字段，供 Gateway 角色加载目标服务时复用。
func validateDiscoveryDefinition(role string, definition ServiceDefinition) error {
	if strings.TrimSpace(definition.Name) == "" {
		return fmt.Errorf("service %q name is required", role)
	}
	if strings.TrimSpace(definition.Address) == "" {
		return fmt.Errorf("service %q address is required", role)
	}
	if strings.TrimSpace(definition.EtcdAddr) == "" {
		return fmt.Errorf("service %q etcd_addr is required", role)
	}
	if _, err := portFromAddress(definition.Address); err != nil {
		return fmt.Errorf("service %q address is invalid", role)
	}
	return nil
}

func gatewayModeOrDefault(mode string) string {
	if mode = strings.TrimSpace(mode); mode != "" {
		return mode
	}
	return "release"
}

// serverModeOrDefault 为业务服务计算默认 Mode：inventory 的 store 为空时按 debug 回退内存存储，
// 与既有独立 YAML 在未显式配置 Mode 时的行为保持一致。
func serverModeOrDefault(store string) string {
	store = strings.TrimSpace(store)
	if store == "" {
		return "debug"
	}
	return "release"
}

func expandNamedField(name string, value *string) error {
	expanded, err := expandEnv(value)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	*value = expanded
	return nil
}

// expandOptionalField 展开可选字段：缺失 ${VAR} 变量时回退为空值，不返回错误。
func expandOptionalField(name string, value *string) error {
	expanded, err := expandOptionalEnv(value)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	*value = expanded
	return nil
}

func expandEnv(value *string) (string, error) {
	return expandWithPolicy(value, true)
}

func expandOptionalEnv(value *string) (string, error) {
	return expandWithPolicy(value, false)
}

// expandWithPolicy 用 os.Expand 展开 ${VAR}。
// required 为 true 时，缺失变量视为错误（用于 name/address/etcd_addr 等必填项）；
// required 为 false 时，缺失变量回退为空字符串（用于可选凭据），缺失错误只包含变量名不包含值。
func expandWithPolicy(value *string, required bool) (string, error) {
	missing := make(map[string]struct{})
	expanded := os.Expand(*value, func(key string) string {
		resolved, ok := os.LookupEnv(key)
		if !ok {
			missing[key] = struct{}{}
			return ""
		}
		return resolved
	})
	if len(missing) == 0 {
		return expanded, nil
	}
	if !required {
		return "", nil
	}
	keys := make([]string, 0, len(missing))
	for key := range missing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return "", fmt.Errorf("missing environment variables: %s", strings.Join(keys, ", "))
}

func portFromAddress(address string) (string, error) {
	_, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || port == "" {
		return "", fmt.Errorf("address must contain host and port")
	}
	return port, nil
}
