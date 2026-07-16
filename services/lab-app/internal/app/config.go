// config.go 作用是定义实验室应用程序的配置结构体，并提供加载配置的函数。它从环境变量中读取配置值，如果环境变量未设置，则使用默认值。
package app

import (
	"fmt"
	"net"
	"time"

	"github.com/go-sql-driver/mysql"

	sharedconfig "website-gobased/internal/config"
)

// Config contains lab application runtime identity and settings.
type Config struct {
	Addr              string
	LabID             string
	InstanceID        string
	ScenarioType      string
	DatabaseDSN       string
	EffectiveCapacity int
	CapacityWindow    time.Duration
}

// LoadConfig loads lab application settings.
func LoadConfig() (Config, error) {
	databaseUser, err := sharedconfig.Required("MYSQL_USER")
	if err != nil {
		return Config{}, err
	}
	databasePassword, err := sharedconfig.Required("MYSQL_PASSWORD")
	if err != nil {
		return Config{}, err
	}
	databaseName, err := sharedconfig.Required("MYSQL_DATABASE")
	if err != nil {
		return Config{}, err
	}
	effectiveCapacity, err := sharedconfig.Int("EFFECTIVE_CAPACITY", 100)
	if err != nil || effectiveCapacity <= 0 {
		return Config{}, fmt.Errorf("EFFECTIVE_CAPACITY must be a positive integer")
	}
	capacityWindowMS, err := sharedconfig.Int("CAPACITY_WINDOW_MS", 1000)
	if err != nil || capacityWindowMS <= 0 {
		return Config{}, fmt.Errorf("CAPACITY_WINDOW_MS must be a positive integer")
	}
	databaseConfig := mysql.NewConfig()
	databaseConfig.User = databaseUser
	databaseConfig.Passwd = databasePassword
	databaseConfig.Net = "tcp"
	databaseConfig.Addr = net.JoinHostPort(
		sharedconfig.String("MYSQL_HOST", "lab-db"),
		sharedconfig.String("MYSQL_PORT", "3306"),
	)
	databaseConfig.DBName = databaseName
	databaseConfig.ParseTime = true
	databaseConfig.Loc = time.UTC
	databaseConfig.Params = map[string]string{
		"charset": "utf8mb4",
	}
	return Config{
		Addr:              sharedconfig.String("LAB_APP_ADDR", ":8080"),
		LabID:             sharedconfig.String("LAB_ID", "unassigned"),
		InstanceID:        sharedconfig.String("INSTANCE_ID", "unassigned"),
		ScenarioType:      sharedconfig.String("SCENARIO_TYPE", "scaffold"),
		DatabaseDSN:       databaseConfig.FormatDSN(),
		EffectiveCapacity: effectiveCapacity,
		CapacityWindow:    time.Duration(capacityWindowMS) * time.Millisecond,
	}, nil
}
