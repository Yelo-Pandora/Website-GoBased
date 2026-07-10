package repository

import (
	"context"
	"errors"

	"website-gobased/internal/domain"
)

var ErrPurchaseNotFound = errors.New("purchase not found")

type CreatePurchaseInput struct {
	UserID      int64
	ProductID   int64
	Quantity    int
	TotalAmount float64
	Currency    string
	Status      string
}

type PurchaseRepository interface {
	Create(context.Context, CreatePurchaseInput) (domain.Purchase, error)
	GetByID(context.Context, int64) (domain.Purchase, error)
	List(context.Context) ([]domain.Purchase, error)
	UpdateStatus(context.Context, int64, string) (domain.Purchase, error)
	Delete(context.Context, int64) error
}
