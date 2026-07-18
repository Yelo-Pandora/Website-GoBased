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
	Addr                   string
	LabID                  string
	InstanceID             string
	ScenarioType           string
	DatabaseDSN            string
	ProcessingSpeed        int
	MaxLoad                int
	RedisAddr              string
	CacheInstanceCount     int
	CacheL1MaxEntries      int
	CacheL1TTL             time.Duration
	CacheL2TTL             time.Duration
	CacheL2JitterPercent   int
	CacheLatencyL1MS       int
	CacheLatencyRedisMS    int
	CacheLatencyMySQLMS    int
	CacheLatencyDegradedMS int
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
	cacheInstanceCount, err := sharedconfig.Int("CACHE_INSTANCE_COUNT", 3)
	if err != nil || cacheInstanceCount <= 0 || cacheInstanceCount > 4 {
		return Config{}, fmt.Errorf("CACHE_INSTANCE_COUNT must be between 1 and 4")
	}
	cacheL1MaxEntries, err := sharedconfig.Int("CACHE_L1_MAX_PRODUCT_ENTRIES", 9)
	if err != nil || cacheL1MaxEntries <= 0 || cacheL1MaxEntries > 64 {
		return Config{}, fmt.Errorf("CACHE_L1_MAX_PRODUCT_ENTRIES must be between 1 and 64")
	}
	cacheL1TTLMS, err := sharedconfig.Int("CACHE_L1_TTL_MS", 15000)
	if err != nil || cacheL1TTLMS <= 0 {
		return Config{}, fmt.Errorf("CACHE_L1_TTL_MS must be positive")
	}
	cacheL2TTLMS, err := sharedconfig.Int("CACHE_L2_TTL_MS", 30000)
	if err != nil || cacheL2TTLMS <= 0 {
		return Config{}, fmt.Errorf("CACHE_L2_TTL_MS must be positive")
	}
	cacheL2Jitter, err := sharedconfig.Int("CACHE_L2_TTL_JITTER_PERCENT", 20)
	if err != nil || cacheL2Jitter < 0 || cacheL2Jitter > 50 {
		return Config{}, fmt.Errorf("CACHE_L2_TTL_JITTER_PERCENT must be between 0 and 50")
	}
	latencyL1, err := sharedconfig.Int("CACHE_LATENCY_L1_MS", 120)
	if err != nil || latencyL1 <= 0 {
		return Config{}, fmt.Errorf("CACHE_LATENCY_L1_MS must be positive")
	}
	latencyRedis, err := sharedconfig.Int("CACHE_LATENCY_REDIS_MS", 320)
	if err != nil || latencyRedis <= 0 {
		return Config{}, fmt.Errorf("CACHE_LATENCY_REDIS_MS must be positive")
	}
	latencyMySQL, err := sharedconfig.Int("CACHE_LATENCY_MYSQL_MS", 800)
	if err != nil || latencyMySQL <= 0 {
		return Config{}, fmt.Errorf("CACHE_LATENCY_MYSQL_MS must be positive")
	}
	latencyDegraded, err := sharedconfig.Int("CACHE_LATENCY_DEGRADED_MS", 500)
	if err != nil || latencyDegraded <= 0 {
		return Config{}, fmt.Errorf("CACHE_LATENCY_DEGRADED_MS must be positive")
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
		Addr:                   sharedconfig.String("LAB_APP_ADDR", ":8080"),
		LabID:                  sharedconfig.String("LAB_ID", "unassigned"),
		InstanceID:             sharedconfig.String("INSTANCE_ID", "unassigned"),
		ScenarioType:           sharedconfig.String("SCENARIO_TYPE", "scaffold"),
		DatabaseDSN:            databaseConfig.FormatDSN(),
		ProcessingSpeed:        processingSpeed,
		MaxLoad:                maxLoad,
		RedisAddr:              sharedconfig.String("REDIS_ADDR", "lab-redis:6379"),
		CacheInstanceCount:     cacheInstanceCount,
		CacheL1MaxEntries:      cacheL1MaxEntries,
		CacheL1TTL:             time.Duration(cacheL1TTLMS) * time.Millisecond,
		CacheL2TTL:             time.Duration(cacheL2TTLMS) * time.Millisecond,
		CacheL2JitterPercent:   cacheL2Jitter,
		CacheLatencyL1MS:       latencyL1,
		CacheLatencyRedisMS:    latencyRedis,
		CacheLatencyMySQLMS:    latencyMySQL,
		CacheLatencyDegradedMS: latencyDegraded,
	}, nil
}
