package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	root "spc"
	"spc/internal/api"
	"spc/internal/migrate"
	"spc/internal/service"
	"spc/internal/store"
)

// migrations 由根包嵌入。
var migrationsFS = root.MigrationsFS

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	dsn := getenv("DATABASE_URL",
		"postgres://spc:spc@localhost:5432/spc?sslmode=disable")
	addr := getenv("HTTP_ADDR", ":8080")

	connStr, err := pqConnString(dsn)
	if err != nil {
		log.Fatalf("解析 DATABASE_URL 失败: %v", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(20)
	if err := waitDB(db); err != nil {
		log.Fatalf("数据库不可用: %v", err)
	}

	if err := migrate.New(db, migrationsFS).Up(context.Background()); err != nil {
		log.Fatalf("迁移失败: %v", err)
	}

	svc := service.New(store.New(db))
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	api.New(svc).Register(e)

	log.Printf("SPC 后端监听 %s", addr)
	if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
