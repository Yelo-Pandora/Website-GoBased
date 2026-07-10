package main

import (
	"log"
	"net/http"

	"website-gobased/internal/app"
	"website-gobased/internal/config"
)

func main() {
	// 加载配置文件
	cfg := config.Load()
	// 初始化服务器
	server, cleanup, err := app.NewServer(cfg)
	// 如果初始化失败，记录错误并退出
	if err != nil {
		log.Fatalf("bootstrap server: %v", err)
	}
	// 确保在程序退出时进行清理操作
	defer func() {
		if err := cleanup(); err != nil {
			log.Printf("cleanup error: %v", err)
		}
	}()

	log.Printf("purchase API listening on %s", cfg.APIAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen and serve: %v", err)
	}
}
