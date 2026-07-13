package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"website-gobased/internal/config"
	"website-gobased/internal/httpapi"
	mysqlrepo "website-gobased/internal/repository/mysql"
	"website-gobased/internal/service"
)

func NewServer(cfg config.Config) (*http.Server, func() error, error) {
	// 打开 MySQL 数据库连接，建立连接句柄db
	db, err := sql.Open("mysql", cfg.MySQLDSN())
	if err != nil {
		return nil, nil, fmt.Errorf("open mysql: %w", err)
	}

	//设置访问上下文的超时时间为5秒，确保在5秒内完成数据库连接的ping操作
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("ping mysql: %w", err)
	}

	// 创建购买仓库和服务，并将其传递给HTTP路由器
	purchaseRepo := mysqlrepo.NewPurchaseRepository(db)
	purchaseService := service.NewPurchaseService(purchaseRepo)
	router := httpapi.NewRouter(purchaseService)

	// 创建HTTP服务器，设置监听地址、路由器和超时时间
	server := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return server, db.Close, nil
}
