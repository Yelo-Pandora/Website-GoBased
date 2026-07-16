// Package app wires the platform API process.
package app

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	sharedconfig "website-gobased/internal/config"
)

const platformDatabaseName = "platform"

// Config contains the platform API runtime settings.
type Config struct {
	Addr                   string
	LabGatewayAddr         string
	DatabaseDSN            string
	OrchestratorSocketPath string
	LabMaxActive           int
	LabMaxTempContainers   int
	LabMaxInstances        int
	LabOperationWorkerID   string
	LabOperationPoll       time.Duration
	LabOperationLease      time.Duration
	LabOrchestratorTimeout time.Duration
	LabIdleTimeout         time.Duration
	LabMaxDuration         time.Duration
	LabExpiringLead        time.Duration
	LabLifecyclePoll       time.Duration
	LabReconcileInterval   time.Duration
	LabReconcileCleanup    bool
	TrafficRequestTimeout  time.Duration
	TrafficRatePerSecond   int
	TrafficBurst           int
	TrafficLimiterEntries  int
	AuthSessionTTL         time.Duration
	AuthCookieName         string
	AuthCookieSecure       bool
	AuthLoginWindow        time.Duration
	AuthLoginMaxFailures   int
	AuthLoginBlockDuration time.Duration
	AuthLoginMaxEntries    int
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

	authSessionTTL, err := duration("AUTH_SESSION_TTL", 8*time.Hour)
	if err != nil {
		return Config{}, err
	}
	authLoginWindow, err := duration("AUTH_LOGIN_WINDOW", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	authLoginBlockDuration, err := duration(
		"AUTH_LOGIN_BLOCK_DURATION",
		10*time.Minute,
	)
	if err != nil {
		return Config{}, err
	}
	authLoginMaxFailures, err := sharedconfig.Int("AUTH_LOGIN_MAX_FAILURES", 5)
	if err != nil || authLoginMaxFailures <= 0 {
		return Config{}, fmt.Errorf("AUTH_LOGIN_MAX_FAILURES must be a positive integer")
	}
	authLoginMaxEntries, err := sharedconfig.Int("AUTH_LOGIN_LIMITER_MAX_ENTRIES", 2048)
	if err != nil || authLoginMaxEntries <= 0 {
		return Config{}, fmt.Errorf("AUTH_LOGIN_LIMITER_MAX_ENTRIES must be a positive integer")
	}
	authCookieName := sharedconfig.String("AUTH_COOKIE_NAME", "session")
	if !validCookieName(authCookieName) {
		return Config{}, fmt.Errorf("AUTH_COOKIE_NAME contains invalid characters")
	}
	authCookieSecure, err := boolean("AUTH_COOKIE_SECURE", false)
	if err != nil {
		return Config{}, err
	}
	labMaxActive, err := positiveInt("LAB_MAX_ACTIVE", 10)
	if err != nil {
		return Config{}, err
	}
	labMaxTempContainers, err := positiveInt("LAB_MAX_TEMP_CONTAINERS", 40)
	if err != nil {
		return Config{}, err
	}
	labMaxInstances, err := positiveInt("LAB_MAX_INSTANCES_PER_SESSION", 4)
	if err != nil {
		return Config{}, err
	}
	labOperationPoll, err := duration("LAB_OPERATION_POLL_INTERVAL", 500*time.Millisecond)
	if err != nil {
		return Config{}, err
	}
	labOperationLease, err := duration("LAB_OPERATION_LEASE_DURATION", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	labOrchestratorTimeout, err := duration("LAB_ORCHESTRATOR_TIMEOUT", 20*time.Second)
	if err != nil {
		return Config{}, err
	}
	if labOperationLease <= labOrchestratorTimeout {
		return Config{}, fmt.Errorf(
			"LAB_OPERATION_LEASE_DURATION must exceed LAB_ORCHESTRATOR_TIMEOUT",
		)
	}
	labIdleTimeout, err := duration("LAB_IDLE_TIMEOUT", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	labMaxDuration, err := duration("LAB_MAX_DURATION", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	labExpiringLead, err := duration("LAB_EXPIRING_LEAD", time.Minute)
	if err != nil {
		return Config{}, err
	}
	if labExpiringLead >= labIdleTimeout || labExpiringLead >= labMaxDuration {
		return Config{}, fmt.Errorf(
			"LAB_EXPIRING_LEAD must be shorter than LAB_IDLE_TIMEOUT and LAB_MAX_DURATION",
		)
	}
	labLifecyclePoll, err := duration("LAB_LIFECYCLE_POLL_INTERVAL", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	labReconcileInterval, err := duration("LAB_RECONCILE_INTERVAL", time.Minute)
	if err != nil {
		return Config{}, err
	}
	labReconcileCleanup, err := boolean("LAB_RECONCILE_CLEANUP", true)
	if err != nil {
		return Config{}, err
	}
	trafficRequestTimeout, err := duration("TRAFFIC_REQUEST_TIMEOUT", 3*time.Second)
	if err != nil {
		return Config{}, err
	}
	trafficRatePerSecond, err := positiveInt("TRAFFIC_RATE_PER_SECOND", 10)
	if err != nil {
		return Config{}, err
	}
	trafficBurst, err := positiveInt("TRAFFIC_BURST", 10)
	if err != nil {
		return Config{}, err
	}
	trafficLimiterEntries, err := positiveInt("TRAFFIC_LIMITER_MAX_ENTRIES", 2048)
	if err != nil {
		return Config{}, err
	}
	labOperationWorkerID := sharedconfig.String(
		"LAB_OPERATION_WORKER_ID",
		sharedconfig.String("HOSTNAME", "platform-api"),
	)
	if !validWorkerID(labOperationWorkerID) {
		return Config{}, fmt.Errorf("LAB_OPERATION_WORKER_ID contains invalid characters")
	}
	orchestratorSocketPath := sharedconfig.String(
		"ORCHESTRATOR_SOCKET_PATH",
		"/run/platform/orchestrator.sock",
	)
	if strings.TrimSpace(orchestratorSocketPath) != orchestratorSocketPath ||
		orchestratorSocketPath == "" {
		return Config{}, fmt.Errorf("ORCHESTRATOR_SOCKET_PATH must not be empty")
	}

	return Config{
		Addr:                   addr,
		LabGatewayAddr:         sharedconfig.String("LAB_GATEWAY_ADDR", "http://lab-gateway-nginx:8080"),
		DatabaseDSN:            dbConfig.FormatDSN(),
		OrchestratorSocketPath: orchestratorSocketPath,
		LabMaxActive:           labMaxActive,
		LabMaxTempContainers:   labMaxTempContainers,
		LabMaxInstances:        labMaxInstances,
		LabOperationWorkerID:   labOperationWorkerID,
		LabOperationPoll:       labOperationPoll,
		LabOperationLease:      labOperationLease,
		LabOrchestratorTimeout: labOrchestratorTimeout,
		LabIdleTimeout:         labIdleTimeout,
		LabMaxDuration:         labMaxDuration,
		LabExpiringLead:        labExpiringLead,
		LabLifecyclePoll:       labLifecyclePoll,
		LabReconcileInterval:   labReconcileInterval,
		LabReconcileCleanup:    labReconcileCleanup,
		TrafficRequestTimeout:  trafficRequestTimeout,
		TrafficRatePerSecond:   trafficRatePerSecond,
		TrafficBurst:           trafficBurst,
		TrafficLimiterEntries:  trafficLimiterEntries,
		AuthSessionTTL:         authSessionTTL,
		AuthCookieName:         authCookieName,
		AuthCookieSecure:       authCookieSecure,
		AuthLoginWindow:        authLoginWindow,
		AuthLoginMaxFailures:   authLoginMaxFailures,
		AuthLoginBlockDuration: authLoginBlockDuration,
		AuthLoginMaxEntries:    authLoginMaxEntries,
	}, nil
}

func positiveInt(key string, fallback int) (int, error) {
	value, err := sharedconfig.Int(key, fallback)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return value, nil
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	value := sharedconfig.String(key, fallback.String())
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return parsed, nil
}

func boolean(key string, fallback bool) (bool, error) {
	value := sharedconfig.String(key, strconv.FormatBool(fallback))
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return parsed, nil
}

func validCookieName(name string) bool {
	if strings.TrimSpace(name) != name || name == "" {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' ||
			r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validWorkerID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}
