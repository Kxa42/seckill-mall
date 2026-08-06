// Command migrate 将 migrations 目录中的未执行 SQL 依次应用到 MySQL。
package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	platformmigrate "seckill-mall/internal/platform/migrate"
)

func main() {
	dsn := os.Getenv("SECKILL_MYSQL_DSN")
	if dsn == "" {
		log.Fatal("SECKILL_MYSQL_DSN 不能为空")
	}
	directory := os.Getenv("SECKILL_MIGRATIONS_DIR")
	if directory == "" {
		directory = "migrations"
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("打开 MySQL: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := platformmigrate.Run(ctx, db, os.DirFS(directory)); err != nil {
		log.Fatalf("执行 migration: %v", err)
	}
	log.Println("migration 执行完成")
}
