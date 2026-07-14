// config.go 作用是定义实验室应用程序的配置结构体，并提供加载配置的函数。它从环境变量中读取配置值，如果环境变量未设置，则使用默认值。
package app

import sharedconfig "website-gobased/internal/config"

// Config contains lab application runtime identity and settings.
type Config struct {
	Addr         string
	LabID        string
	InstanceID   string
	ScenarioType string
}

// LoadConfig loads lab application settings.
func LoadConfig() Config {
	return Config{
		Addr:         sharedconfig.String("LAB_APP_ADDR", ":8080"),
		LabID:        sharedconfig.String("LAB_ID", "unassigned"),
		InstanceID:   sharedconfig.String("INSTANCE_ID", "unassigned"),
		ScenarioType: sharedconfig.String("SCENARIO_TYPE", "scaffold"),
	}
}
