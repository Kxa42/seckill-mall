// Command api-gateway 启动商城统一 HTTP Gateway。
package main

import (
	"log"

	"seckill-mall/services/gateway/internal/app"
)

func main() {
	if err := gateway.Run(); err != nil {
		log.Fatalf("api gateway stopped: %v", err)
	}
}
