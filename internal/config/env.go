// env.go 提供了用于从环境变量中检索配置值的实用函数。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// String 返回一个字符串环境变量的值，如果该值为空，则返回提供的回退值。
func String(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// Required 返回一个非空的环境变量值，如果该值为空，则返回错误。
func Required(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("environment variable %s is required", key)
	}
	return value, nil
}

// Int 返回一个整数环境变量的值，如果该值为空，则返回提供的回退值，如果无法解析为整数，则返回错误。
func Int(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse environment variable %s: %w", key, err)
	}
	return parsed, nil
}
