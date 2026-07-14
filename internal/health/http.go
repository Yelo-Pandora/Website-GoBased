// 用于健康检查的HTTP处理程序和响应结构。
package health

import (
	"encoding/json"
	"net/http"
	"time"
)

// 健康检查响应结构。
type Response struct {
	Service   string         `json:"service"`
	Status    string         `json:"status"`
	Timestamp time.Time      `json:"timestamp"`
	Details   map[string]any `json:"details,omitempty"`
}

// Write 将健康检查响应写入HTTP响应。
func Write(w http.ResponseWriter, statusCode int, response Response) error {
	response.Timestamp = time.Now().UTC()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(response)
}
