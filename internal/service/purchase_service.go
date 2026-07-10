package service

import (
	"context"
	"errors"
	"strings"

	"website-gobased/internal/domain"
	"website-gobased/internal/repository"
)

var ErrInvalidPurchaseInput = errors.New("invalid purchase input")
var ErrInvalidPurchaseState = errors.New("invalid purchase status")

var allowedStatuses = map[string]struct{}{
	"pending":   {},
	"paid":      {},
	"cancelled": {},
	"refunded":  {},
}

type PurchaseService struct {
	repo repository.PurchaseRepository
}

func NewPurchaseService(repo repository.PurchaseRepository) *PurchaseService {
	return &PurchaseService{repo: repo}
}

func (s *PurchaseService) Create(
	ctx context.Context,
	input repository.CreatePurchaseInput,
) (domain.Purchase, error) {
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))

	if input.UserID <= 0 ||
		input.ProductID <= 0 ||
		input.Quantity <= 0 ||
		input.TotalAmount <= 0 ||
		input.Currency == "" {
		return domain.Purchase{}, ErrInvalidPurchaseInput
	}

	if input.Status == "" {
		input.Status = "pending"
	}
	if !isValidStatus(input.Status) {
		return domain.Purchase{}, ErrInvalidPurchaseState
	}

	return s.repo.Create(ctx, input)
}

func (s *PurchaseService) GetByID(
	ctx context.Context,
	id int64,
) (domain.Purchase, error) {
	if id <= 0 {
		return domain.Purchase{}, repository.ErrPurchaseNotFound
	}
	return s.repo.GetByID(ctx, id)
}

func (s *PurchaseService) List(
	ctx context.Context,
) ([]domain.Purchase, error) {
	return s.repo.List(ctx)
}

func (s *PurchaseService) UpdateStatus(
	ctx context.Context,
	id int64,
	status string,
) (domain.Purchase, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if id <= 0 {
		return domain.Purchase{}, repository.ErrPurchaseNotFound
	}
	if !isValidStatus(status) {
		return domain.Purchase{}, ErrInvalidPurchaseState
	}
	return s.repo.UpdateStatus(ctx, id, status)
}

func (s *PurchaseService) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return repository.ErrPurchaseNotFound
	}
	return s.repo.Delete(ctx, id)
}

func isValidStatus(status string) bool {
	_, ok := allowedStatuses[status]
	return ok
}
