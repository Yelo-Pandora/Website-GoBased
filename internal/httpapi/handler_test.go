package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"website-gobased/internal/domain"
	"website-gobased/internal/repository"
	"website-gobased/internal/service"
)

type fakePurchaseRepository struct {
	purchases map[int64]domain.Purchase
	nextID    int64
}

func newFakePurchaseRepository() *fakePurchaseRepository {
	return &fakePurchaseRepository{
		purchases: make(map[int64]domain.Purchase),
		nextID:    1,
	}
}

func (r *fakePurchaseRepository) Create(
	_ context.Context,
	input repository.CreatePurchaseInput,
) (domain.Purchase, error) {
	now := time.Now().UTC()
	purchase := domain.Purchase{
		ID:          r.nextID,
		UserID:      input.UserID,
		ProductID:   input.ProductID,
		Quantity:    input.Quantity,
		TotalAmount: input.TotalAmount,
		Currency:    input.Currency,
		Status:      input.Status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	r.purchases[purchase.ID] = purchase
	r.nextID++
	return purchase, nil
}

func (r *fakePurchaseRepository) GetByID(
	_ context.Context,
	id int64,
) (domain.Purchase, error) {
	purchase, ok := r.purchases[id]
	if !ok {
		return domain.Purchase{}, repository.ErrPurchaseNotFound
	}
	return purchase, nil
}

func (r *fakePurchaseRepository) List(
	_ context.Context,
) ([]domain.Purchase, error) {
	purchases := make([]domain.Purchase, 0, len(r.purchases))
	for _, purchase := range r.purchases {
		purchases = append(purchases, purchase)
	}
	return purchases, nil
}

func (r *fakePurchaseRepository) UpdateStatus(
	_ context.Context,
	id int64,
	status string,
) (domain.Purchase, error) {
	purchase, ok := r.purchases[id]
	if !ok {
		return domain.Purchase{}, repository.ErrPurchaseNotFound
	}
	purchase.Status = status
	purchase.UpdatedAt = time.Now().UTC()
	r.purchases[id] = purchase
	return purchase, nil
}

func (r *fakePurchaseRepository) Delete(
	_ context.Context,
	id int64,
) error {
	if _, ok := r.purchases[id]; !ok {
		return repository.ErrPurchaseNotFound
	}
	delete(r.purchases, id)
	return nil
}

func TestPurchaseLifecycle(t *testing.T) {
	repo := newFakePurchaseRepository()
	svc := service.NewPurchaseService(repo)
	server := httptest.NewServer(NewRouter(svc))
	defer server.Close()

	createBody := map[string]any{
		"user_id":      1,
		"product_id":   1001,
		"quantity":     2,
		"total_amount": 99.90,
		"currency":     "cny",
	}
	payload, err := json.Marshal(createBody)
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}

	createResp, err := http.Post(
		server.URL+"/api/v1/purchases",
		"application/json",
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("create purchase: %v", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("unexpected create status: got %d", createResp.StatusCode)
	}

	var created domain.Purchase
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created purchase: %v", err)
	}

	if created.ID == 0 {
		t.Fatal("expected created purchase id")
	}
	if created.Status != "pending" {
		t.Fatalf("expected default status pending, got %q", created.Status)
	}

	getResp, err := http.Get(server.URL + "/api/v1/purchases/" + "1")
	if err != nil {
		t.Fatalf("get purchase: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get status: got %d", getResp.StatusCode)
	}

	var fetched domain.Purchase
	if err := json.NewDecoder(getResp.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode fetched purchase: %v", err)
	}
	if fetched.ProductID != 1001 {
		t.Fatalf("unexpected product id: got %d", fetched.ProductID)
	}

	listResp, err := http.Get(server.URL + "/api/v1/purchases")
	if err != nil {
		t.Fatalf("list purchases: %v", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected list status: got %d", listResp.StatusCode)
	}

	var purchases []domain.Purchase
	if err := json.NewDecoder(listResp.Body).Decode(&purchases); err != nil {
		t.Fatalf("decode purchases: %v", err)
	}
	if len(purchases) != 1 {
		t.Fatalf("expected 1 purchase, got %d", len(purchases))
	}

	patchBody, err := json.Marshal(map[string]string{"status": "paid"})
	if err != nil {
		t.Fatalf("marshal patch body: %v", err)
	}
	patchReq, err := http.NewRequest(
		http.MethodPatch,
		server.URL+"/api/v1/purchases/1/status",
		bytes.NewReader(patchBody),
	)
	if err != nil {
		t.Fatalf("new patch request: %v", err)
	}
	patchReq.Header.Set("Content-Type", "application/json")

	patchResp, err := http.DefaultClient.Do(patchReq)
	if err != nil {
		t.Fatalf("patch purchase: %v", err)
	}
	defer patchResp.Body.Close()

	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected patch status: got %d", patchResp.StatusCode)
	}
}
