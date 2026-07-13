package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/domain"
	"website-gobased/internal/repository"
	"website-gobased/internal/service"
)

type PurchaseService interface {
	Create(context.Context, repository.CreatePurchaseInput) (domain.Purchase, error)
	GetByID(context.Context, int64) (domain.Purchase, error)
	List(context.Context) ([]domain.Purchase, error)
	UpdateStatus(context.Context, int64, string) (domain.Purchase, error)
	Delete(context.Context, int64) error
}

// PurchaseHandler 处理购买相关的 HTTP 请求，内置了 PurchaseService 接口，用于处理购买相关的业务逻辑。
type PurchaseHandler struct {
	purchaseService PurchaseService
}

// createPurchaseRequest 用于解析创建购买请求的 JSON 数据。
type createPurchaseRequest struct {
	UserID      int64   `json:"user_id"`
	ProductID   int64   `json:"product_id"`
	Quantity    int     `json:"quantity"`
	TotalAmount float64 `json:"total_amount"`
	Currency    string  `json:"currency"`
	Status      string  `json:"status"`
}

// updatePurchaseStatusRequest 用于解析更新购买状态请求的 JSON 数据。
type updatePurchaseStatusRequest struct {
	Status string `json:"status"`
}

// NewPurchaseHandler 创建一个新的 PurchaseHandler 实例，接收一个实现了 PurchaseService 接口的服务对象。
func NewPurchaseHandler(purchaseService PurchaseService) *PurchaseHandler {
	return &PurchaseHandler{purchaseService: purchaseService}
}

// List 处理获取所有购买记录的 HTTP 请求。
func (h *PurchaseHandler) List(c *gin.Context) {
	purchases, err := h.purchaseService.List(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, purchases)
}

// Create 处理创建购买记录的 HTTP 请求。
func (h *PurchaseHandler) Create(c *gin.Context) {
	var req createPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	purchase, err := h.purchaseService.Create(
		c.Request.Context(),
		repository.CreatePurchaseInput{
			UserID:      req.UserID,
			ProductID:   req.ProductID,
			Quantity:    req.Quantity,
			TotalAmount: req.TotalAmount,
			Currency:    req.Currency,
			Status:      req.Status,
		},
	)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, purchase)
}

func (h *PurchaseHandler) GetByID(c *gin.Context) {
	id, err := parseInt64PathParam(c, "id")
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid purchase id")
		return
	}

	purchase, err := h.purchaseService.GetByID(c.Request.Context(), id)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, purchase)
}

func (h *PurchaseHandler) UpdateStatus(c *gin.Context) {
	id, err := parseInt64PathParam(c, "id")
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid purchase id")
		return
	}

	var req updatePurchaseStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	purchase, err := h.purchaseService.UpdateStatus(
		c.Request.Context(),
		id,
		req.Status,
	)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, purchase)
}

func (h *PurchaseHandler) Delete(c *gin.Context) {
	id, err := parseInt64PathParam(c, "id")
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid purchase id")
		return
	}

	if err := h.purchaseService.Delete(c.Request.Context(), id); err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *PurchaseHandler) handleServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repository.ErrPurchaseNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrInvalidPurchaseInput),
		errors.Is(err, service.ErrInvalidPurchaseState):
		writeError(c, http.StatusBadRequest, err.Error())
	default:
		writeError(c, http.StatusInternalServerError, err.Error())
	}
}

func parseInt64PathParam(c *gin.Context, key string) (int64, error) {
	return strconv.ParseInt(c.Param(key), 10, 64)
}
