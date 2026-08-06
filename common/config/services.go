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
	PurchaseLimit int32  `mapstructure:"purchase_limit"`
}

// GatewayDefinition 描述统一配置模板中的 Gateway 运行配置。
type GatewayDefinition struct {
	Name        string `mapstructure:"name"`
	HTTPAddress string `mapstructure:"http_address"`
	EtcdAddr    string `mapstructure:"etcd_addr"`
	CommerceURL string `mapstructure:"commerce_url"`
}

type serviceManifest struct {
	Services map[string]ServiceDefinition `mapstructure:"services"`
	Gateway  GatewayDefinition            `mapstructure:"gateway"`
}

// LoadServiceRuntimeConfig 从统一商城配置模板加载指定角色。
// 只展开当前角色需要的敏感环境变量，避免 Gateway 或单个业务服务读取其他服务的 DSN。
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
		port, err := portFromAddress(manifest.Gateway.HTTPAddress)
		if err != nil {
			return nil, fmt.Errorf("gateway http_address is invalid")
		}
		cfg.Server = ServerConfig{Name: manifest.Gateway.Name, Port: port}
		cfg.Etcd.Addr = manifest.Gateway.EtcdAddr
		cfg.Commerce.URL = manifest.Gateway.CommerceURL
		for _, serviceRole := range []string{"catalog", "inventory"} {
			definition, ok := manifest.Services[serviceRole]
			if !ok {
				return nil, fmt.Errorf("service %q is missing from configuration", serviceRole)
			}
			if err := expandDiscoveryDefinition(&definition); err != nil {
				return nil, fmt.Errorf("service %q configuration: %w", serviceRole, err)
			}
			if err := validateServiceDefinition(serviceRole, definition); err != nil {
				return nil, err
			}
			if serviceRole == "catalog" {
				cfg.Catalog.ServiceName = definition.Name
				cfg.Catalog.Address = definition.Address
			} else {
				cfg.Inventory.ServiceName = definition.Name
				cfg.Inventory.Address = definition.Address
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
	cfg.Server = ServerConfig{Name: definition.Name}
	cfg.Server.Port, err = portFromAddress(definition.Address)
	if err != nil {
		return nil, fmt.Errorf("service %q address is invalid", role)
	}
	cfg.MySQL.DSN = definition.MySQLDSN
	cfg.MQ.URL = definition.RabbitMQURL
	cfg.Etcd.Addr = definition.EtcdAddr
	cfg.Redis = RedisConfig{Addr: definition.RedisAddr, Password: definition.RedisPassword, DB: definition.RedisDB}

	switch role {
	case "catalog":
		cfg.Catalog = CatalogConfig{ServiceName: definition.Name, Address: definition.Address, MySQLDSN: definition.MySQLDSN}
	case "inventory":
		cfg.Inventory = InventoryConfig{
			ServiceName: definition.Name, Address: definition.Address, Store: definition.Store,
			RedisAddr: definition.RedisAddr, RedisPassword: definition.RedisPassword,
			RedisDB: definition.RedisDB, PurchaseLimit: definition.PurchaseLimit,
		}
		cfg.Seckill.PurchaseLimit = int64(definition.PurchaseLimit)
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

func expandServiceDefinition(definition *ServiceDefinition) error {
	if err := expandDiscoveryDefinition(definition); err != nil {
		return err
	}
	if err := expandNamedField("mysql_dsn", &definition.MySQLDSN); err != nil {
		return err
	}
	if err := expandNamedField("rabbitmq_url", &definition.RabbitMQURL); err != nil {
		return err
	}
	if err := expandNamedField("redis_password", &definition.RedisPassword); err != nil {
		return err
	}
	return nil
}

func expandDiscoveryDefinition(definition *ServiceDefinition) error {
	if err := expandNamedField("name", &definition.Name); err != nil {
		return err
	}
	if err := expandNamedField("address", &definition.Address); err != nil {
		return err
	}
	if err := expandNamedField("etcd_addr", &definition.EtcdAddr); err != nil {
		return err
	}
	return nil
}

func expandGatewayDefinition(definition *GatewayDefinition) error {
	if err := expandNamedField("gateway name", &definition.Name); err != nil {
		return err
	}
	if err := expandNamedField("gateway http_address", &definition.HTTPAddress); err != nil {
		return err
	}
	if err := expandNamedField("gateway etcd_addr", &definition.EtcdAddr); err != nil {
		return err
	}
	if err := expandNamedField("gateway commerce_url", &definition.CommerceURL); err != nil {
		return err
	}
	return nil
}

func expandNamedField(name string, value *string) error {
	expanded, err := expandEnv(value)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	*value = expanded
	return nil
}

func expandEnv(value *string) (string, error) {
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
	keys := make([]string, 0, len(missing))
	for key := range missing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return "", fmt.Errorf("missing environment variables: %s", strings.Join(keys, ", "))
}

func validateServiceDefinition(role string, definition ServiceDefinition) error {
	if strings.TrimSpace(definition.Name) == "" {
		return fmt.Errorf("service %q name is required", role)
	}
	if strings.TrimSpace(definition.Address) == "" {
		return fmt.Errorf("service %q address is required", role)
	}
	if _, err := portFromAddress(definition.Address); err != nil {
		return fmt.Errorf("service %q address is invalid", role)
	}
	return nil
}

func portFromAddress(address string) (string, error) {
	_, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || port == "" {
		return "", fmt.Errorf("address must contain host and port")
	}
	return port, nil
}
