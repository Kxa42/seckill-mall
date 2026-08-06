package tracer

import (
	"os"
	"strings"
)

// EndpointFromEnv 返回 OTLP HTTP 端点，默认兼容现有本地启动方式。
func EndpointFromEnv() string {
	if endpoint := strings.TrimSpace(os.Getenv("SECKILL_OTEL_ENDPOINT")); endpoint != "" {
		return endpoint
	}
	return "localhost:4318"
}
