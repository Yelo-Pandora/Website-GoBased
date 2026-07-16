// Package app wires the orchestrator process.
package app

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	sharedconfig "website-gobased/internal/config"
)

const platformDatabaseName = "platform"

// Config contains validated orchestrator runtime settings.
type Config struct {
	SocketPath       string
	TemplateRoot     string
	DockerHost       string
	DockerAPIVersion string
	DockerTimeout    time.Duration
	DatabaseDSN      string
	DatabaseSecret   string
	ComposeProject   string
	GatewayService   string
	MySQLService     string
	LabAppImage      string
	RedisImage       string
}

// LoadConfig loads and validates orchestrator settings.
func LoadConfig() (Config, error) {
	databaseUser, err := sharedconfig.Required("MYSQL_ORCHESTRATOR_USER")
	if err != nil {
		return Config{}, err
	}
	databasePassword, err := sharedconfig.Required("MYSQL_ORCHESTRATOR_PASSWORD")
	if err != nil {
		return Config{}, err
	}
	databaseName, err := sharedconfig.Required("MYSQL_PLATFORM_DATABASE")
	if err != nil {
		return Config{}, err
	}
	if databaseName != platformDatabaseName {
		return Config{}, fmt.Errorf("MYSQL_PLATFORM_DATABASE must be %q", platformDatabaseName)
	}
	databaseConfig := mysql.NewConfig()
	databaseConfig.User = databaseUser
	databaseConfig.Passwd = databasePassword
	databaseConfig.Net = "tcp"
	databaseConfig.Addr = net.JoinHostPort(
		sharedconfig.String("MYSQL_HOST", "shared-mysql"),
		sharedconfig.String("MYSQL_PORT", "3306"),
	)
	databaseConfig.DBName = databaseName
	databaseConfig.ParseTime = true
	databaseConfig.Loc = time.UTC
	databaseConfig.Params = map[string]string{
		"charset": "utf8mb4", "collation": "utf8mb4_0900_ai_ci",
	}
	dockerTimeout, err := positiveDuration("ORCHESTRATOR_DOCKER_TIMEOUT", 20*time.Second)
	if err != nil {
		return Config{}, err
	}
	socketPath := sharedconfig.String("ORCHESTRATOR_SOCKET_PATH", "/run/platform/orchestrator.sock")
	templateRoot := sharedconfig.String("ORCHESTRATOR_TEMPLATE_ROOT", "/opt/platform/configs")
	composeProject, err := sharedconfig.Required("COMPOSE_PROJECT_NAME")
	if err != nil {
		return Config{}, err
	}
	labAppImage, err := sharedconfig.Required("LAB_APP_IMAGE")
	if err != nil {
		return Config{}, err
	}
	redisImage, err := sharedconfig.Required("REDIS_IMAGE")
	if err != nil {
		return Config{}, err
	}
	config := Config{
		SocketPath:       socketPath,
		TemplateRoot:     templateRoot,
		DockerHost:       sharedconfig.String("DOCKER_HOST", "tcp://docker-socket-proxy:2375"),
		DockerAPIVersion: sharedconfig.String("DOCKER_API_VERSION", "1.47"),
		DockerTimeout:    dockerTimeout,
		DatabaseDSN:      databaseConfig.FormatDSN(),
		DatabaseSecret:   sharedconfig.String("LAB_DATABASE_PASSWORD_SECRET", databasePassword),
		ComposeProject:   composeProject,
		GatewayService:   sharedconfig.String("LAB_GATEWAY_COMPOSE_SERVICE", "lab-gateway-nginx"),
		MySQLService:     sharedconfig.String("MYSQL_COMPOSE_SERVICE", "shared-mysql"),
		LabAppImage:      labAppImage,
		RedisImage:       redisImage,
	}
	if err := validateConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validateConfig(config Config) error {
	values := map[string]string{
		"ORCHESTRATOR_SOCKET_PATH":    config.SocketPath,
		"ORCHESTRATOR_TEMPLATE_ROOT":  config.TemplateRoot,
		"DOCKER_HOST":                 config.DockerHost,
		"DOCKER_API_VERSION":          config.DockerAPIVersion,
		"COMPOSE_PROJECT_NAME":        config.ComposeProject,
		"LAB_GATEWAY_COMPOSE_SERVICE": config.GatewayService,
		"MYSQL_COMPOSE_SERVICE":       config.MySQLService,
	}
	for key, value := range values {
		if strings.TrimSpace(value) != value || value == "" {
			return fmt.Errorf("%s must not be empty or padded", key)
		}
	}
	if len(config.DatabaseSecret) < 16 {
		return errors.New("LAB_DATABASE_PASSWORD_SECRET must contain at least 16 characters")
	}
	return nil
}

func positiveDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := sharedconfig.String(key, fallback.String())
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return parsed, nil
}
