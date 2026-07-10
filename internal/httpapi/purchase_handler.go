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

type PurchaseHandler struct {
	purchaseService PurchaseService
}

type createPurchaseRequest struct {
	UserID      int64   `json:"user_id"`
	ProductID   int64   `json:"product_id"`
	Quantity    int     `json:"quantity"`
	TotalAmount float64 `json:"total_amount"`
	Currency    string  `json:"currency"`
	Status      string  `json:"status"`
}

type updatePurchaseStatusRequest struct {
	Status string `json:"status"`
}

func NewPurchaseHandler(purchaseService PurchaseService) *PurchaseHandler {
	return &PurchaseHandler{purchaseService: purchaseService}
}

func (h *PurchaseHandler) List(c *gin.Context) {
	purchases, err := h.purchaseService.List(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, purchases)
}

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
