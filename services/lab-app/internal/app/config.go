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
	Addr            string
	LabID           string
	InstanceID      string
	ScenarioType    string
	DatabaseDSN     string
	ProcessingSpeed int
	MaxLoad         int
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
	processingSpeed, err := sharedconfig.Int("PROCESSING_SPEED", 20)
	if err != nil || processingSpeed <= 0 {
		return Config{}, fmt.Errorf("PROCESSING_SPEED must be a positive integer")
	}
	maxLoad, err := sharedconfig.Int("MAX_LOAD", 100)
	if err != nil || maxLoad <= 0 {
		return Config{}, fmt.Errorf("MAX_LOAD must be a positive integer")
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
		Addr:            sharedconfig.String("LAB_APP_ADDR", ":8080"),
		LabID:           sharedconfig.String("LAB_ID", "unassigned"),
		InstanceID:      sharedconfig.String("INSTANCE_ID", "unassigned"),
		ScenarioType:    sharedconfig.String("SCENARIO_TYPE", "scaffold"),
		DatabaseDSN:     databaseConfig.FormatDSN(),
		ProcessingSpeed: processingSpeed,
		MaxLoad:         maxLoad,
	}, nil
}
