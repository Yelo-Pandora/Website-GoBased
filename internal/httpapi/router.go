package httpapi

import "github.com/gin-gonic/gin"

func NewRouter(purchaseService PurchaseService) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), RequestID(), gin.Recovery())

	healthHandler := NewHealthHandler()
	purchaseHandler := NewPurchaseHandler(purchaseService)

	router.GET("/healthz", healthHandler.Check)

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
