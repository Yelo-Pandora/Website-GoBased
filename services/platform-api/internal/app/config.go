// Package app wires the platform API process.
package app

import (
	"fmt"
	"net"
	"time"

	"github.com/go-sql-driver/mysql"

	sharedconfig "website-gobased/internal/config"
)

const platformDatabaseName = "platform"

// Config contains the platform API runtime settings.
type Config struct {
	Addr           string
	LabGatewayAddr string
	DatabaseDSN    string
}

// LoadConfig loads and validates platform API settings.
func LoadConfig() (Config, error) {
	user, err := sharedconfig.Required("MYSQL_PLATFORM_USER")
	if err != nil {
		return Config{}, err
	}
	password, err := sharedconfig.Required("MYSQL_PLATFORM_PASSWORD")
	if err != nil {
		return Config{}, err
	}
	database, err := sharedconfig.Required("MYSQL_PLATFORM_DATABASE")
	if err != nil {
		return Config{}, err
	}
	if database != platformDatabaseName {
		return Config{}, fmt.Errorf(
			"MYSQL_PLATFORM_DATABASE must be %q",
			platformDatabaseName,
		)
	}

	dbConfig := mysql.NewConfig()
	dbConfig.User = user
	dbConfig.Passwd = password
	dbConfig.Net = "tcp"
	dbConfig.Addr = net.JoinHostPort(
		sharedconfig.String("MYSQL_HOST", "shared-mysql"),
		sharedconfig.String("MYSQL_PORT", "3306"),
	)
	dbConfig.DBName = database
	dbConfig.ParseTime = true
	dbConfig.Loc = time.UTC
	dbConfig.Params = map[string]string{
		"charset":   "utf8mb4",
		"collation": "utf8mb4_0900_ai_ci",
	}

	addr := sharedconfig.String("PLATFORM_API_ADDR", ":8080")
	if _, err := net.ResolveTCPAddr("tcp", addr); err != nil {
		return Config{}, fmt.Errorf("validate PLATFORM_API_ADDR: %w", err)
	}

	return Config{
		Addr:           addr,
		LabGatewayAddr: sharedconfig.String("LAB_GATEWAY_ADDR", "http://lab-gateway-nginx:8080"),
		DatabaseDSN:    dbConfig.FormatDSN(),
	}, nil
}
