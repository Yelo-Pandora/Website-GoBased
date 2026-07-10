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
	db, err := sql.Open("mysql", cfg.MySQLDSN())
	if err != nil {
		return nil, nil, fmt.Errorf("open mysql: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("ping mysql: %w", err)
	}

	purchaseRepo := mysqlrepo.NewPurchaseRepository(db)
	purchaseService := service.NewPurchaseService(purchaseRepo)
	router := httpapi.NewRouter(purchaseService)

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
