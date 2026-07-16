package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"website-gobased/services/orchestrator/internal/control"
	"website-gobased/services/orchestrator/internal/dockerapi"
	"website-gobased/services/orchestrator/internal/labdb"
	"website-gobased/services/orchestrator/internal/nginx"
	templates "website-gobased/services/orchestrator/internal/template"
	"website-gobased/services/orchestrator/internal/transport/uds"
)

// Run starts the orchestrator after validating all privileged dependencies.
func Run(ctx context.Context, config Config, logger *slog.Logger) error {
	registry, err := templates.Load(config.TemplateRoot)
	if err != nil {
		return fmt.Errorf("load orchestrator templates: %w", err)
	}
	docker, err := dockerapi.NewClient(
		config.DockerHost,
		config.DockerAPIVersion,
		config.DockerTimeout,
	)
	if err != nil {
		return fmt.Errorf("configure Docker API client: %w", err)
	}
	if err := docker.Ping(ctx); err != nil {
		return fmt.Errorf("ping Docker Socket Proxy: %w", err)
	}
	database, err := sql.Open("mysql", config.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("open orchestrator database: %w", err)
	}
	defer database.Close()
	database.SetConnMaxLifetime(3 * time.Minute)
	database.SetMaxIdleConns(2)
	database.SetMaxOpenConns(4)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = database.PingContext(pingCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("ping orchestrator database: %w", err)
	}
	nginxTemplate, ok := registry.Nginx("lab_nginx_fragment_v1")
	if !ok {
		return fmt.Errorf("required nginx template %q is missing", "lab_nginx_fragment_v1")
	}
	nginxManager, err := nginx.NewManager(docker, nginx.Config{
		TemplatePath:    nginxTemplate.TemplatePath,
		OutputDirectory: nginxTemplate.OutputDirectory,
		ComposeProject:  config.ComposeProject,
		GatewayService:  config.GatewayService,
	})
	if err != nil {
		return fmt.Errorf("configure nginx manager: %w", err)
	}
	executor, err := control.NewService(
		logger,
		registry,
		docker,
		labdb.NewProvisioner(database),
		nginxManager,
		control.Config{
			ComposeProject: config.ComposeProject,
			GatewayService: config.GatewayService,
			MySQLService:   config.MySQLService,
			LabAppImage:    config.LabAppImage,
			RedisImage:     config.RedisImage,
			DatabaseSecret: config.DatabaseSecret,
		},
	)
	if err != nil {
		return fmt.Errorf("configure orchestrator control service: %w", err)
	}
	return uds.Run(ctx, config.SocketPath, logger, executor)
}
