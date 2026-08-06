// Command commerce-api 启动完整商城后端 HTTP API 与超时关单任务。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"seckill-mall/internal/commerce"
	"seckill-mall/internal/commerce/httpapi"
	platformauth "seckill-mall/internal/platform/auth"
	platformconfig "seckill-mall/internal/platform/config"
)

func main() {
	cfg, err := platformconfig.LoadCommerce()
	if err != nil {
		log.Fatalf("加载 commerce-api 配置: %v", err)
	}

	repository, readiness, closeStore := initializeRepository(cfg)
	defer closeStore()
	authManager, err := platformauth.NewManager(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	if err != nil {
		log.Fatalf("创建认证管理器: %v", err)
	}
	service, err := commerce.NewService(repository, authManager, cfg.MockPaymentSecret, cfg.OrderTTL)
	if err != nil {
		log.Fatalf("创建商城服务: %v", err)
	}
	closeOrderClient := func() {}
	var orderClient commerce.OrderLifecycleClient
	if cfg.OrderWriteMode == commerce.OrderWriteModeOrderService {
		connection, client, dialErr := commerce.DialOrderLifecycle(cfg.OrderServiceAddr, cfg.InternalCallSecret)
		if dialErr != nil {
			log.Fatalf("连接 Order Service: %v", dialErr)
		}
		orderClient = client
		closeOrderClient = func() {
			if closeErr := connection.Close(); closeErr != nil {
				log.Printf("关闭 Order Service 连接失败: %v", closeErr)
			}
		}
	}
	defer closeOrderClient()
	if err := service.ConfigureOrderMigration(cfg.OrderWriteMode, orderClient); err != nil {
		log.Fatalf("配置订单迁移模式: %v", err)
	}
	if err := service.EnsureAdmin(context.Background(), cfg.AdminEmail, cfg.AdminPassword); err != nil {
		log.Fatalf("初始化管理员: %v", err)
	}

	router, err := httpapi.NewRouter(httpapi.RouterConfig{
		Service:   service,
		Auth:      authManager,
		Readiness: readiness,
	})
	if err != nil {
		log.Fatalf("创建 HTTP Router: %v", err)
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if service.LegacyOrderWritesEnabled() {
		go runExpiryWorker(ctx, service)
	} else {
		log.Println("commerce-api 订单写入与超时关单任务已关闭，由 Order Service 接管")
	}
	go func() {
		log.Printf("commerce-api 已启动 addr=%s", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("commerce-api 停止: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("commerce-api 优雅停机失败: %v", err)
	}
}

func initializeRepository(cfg platformconfig.Commerce) (commerce.Repository, func(context.Context) error, func()) {
	if cfg.StoreDriver == "memory" {
		log.Println("commerce-api 使用临时内存存储，仅适用于开发和验收")
		return commerce.NewMemoryRepository(), func(context.Context) error { return nil }, func() {}
	}

	db, err := gorm.Open(mysql.Open(cfg.MySQLDSN), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("连接 MySQL: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("获取 MySQL 连接池: %v", err)
	}
	sqlDB.SetMaxOpenConns(40)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	repository, err := commerce.NewMySQLRepository(db)
	if err != nil {
		log.Fatalf("创建 Repository: %v", err)
	}
	return repository, sqlDB.PingContext, func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("关闭 MySQL 连接池: %v", err)
		}
	}
}

func runExpiryWorker(ctx context.Context, service *commerce.Service) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := service.ExpireOrders(ctx, 100)
			if err != nil {
				log.Printf("超时关单失败: %v", err)
			} else if count > 0 {
				log.Printf("超时关单完成 count=%d", count)
			}
		}
	}
}
