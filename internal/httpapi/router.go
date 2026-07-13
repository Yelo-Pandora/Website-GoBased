package httpapi

import "github.com/gin-gonic/gin"

func NewRouter(purchaseService PurchaseService) *gin.Engine {
	// 创建一个新的Gin路由器实例，并添加日志记录、中间件和恢复机制
	router := gin.New()
	router.Use(gin.Logger(), RequestID(), gin.Recovery())

	// 创建健康检查处理器和购买处理器
	healthHandler := NewHealthHandler()
	purchaseHandler := NewPurchaseHandler(purchaseService)

	// 定义健康检查路由
	router.GET("/healthz", healthHandler.Check)

	// 定义API路由组，并将购买相关的路由注册到该组中
	api := router.Group("/api/v1")
	{
		purchases := api.Group("/purchases")
		{
			purchases.GET("", purchaseHandler.List)
			purchases.POST("", purchaseHandler.Create)
			purchases.GET("/:id", purchaseHandler.GetByID)
			purchases.PATCH("/:id/status", purchaseHandler.UpdateStatus)
			purchases.DELETE("/:id", purchaseHandler.Delete)
		}
	}

	return router
}
