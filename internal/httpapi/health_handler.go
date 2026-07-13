package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 一个空的健康检查处理器结构体
type HealthHandler struct{}

// NewHealthHandler 创建一个新的健康检查处理器实例
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Check 方法用于处理健康检查请求，返回一个简单的JSON响应，表示服务状态为“ok”
func (h *HealthHandler) Check(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
