// Package product reads the isolated lab product catalog.
package product

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNotFound indicates that a requested product does not exist.
var ErrNotFound = errors.New("product not found")

// Repository reads products from one lab database.
type Repository struct {
	database *sql.DB
}

// NewRepository returns a product repository bound to one lab database.
func NewRepository(database *sql.DB) *Repository {
	return &Repository{database: database}
}

// Require verifies that a product exists.
func (r *Repository) Require(ctx context.Context, productID uint64) error {
	if productID == 0 {
		return ErrNotFound
	}
	var value uint64
	err := r.database.QueryRowContext(
		ctx,
		"SELECT id FROM products WHERE id = ?",
		productID,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read product: %w", err)
	}
	return nil
}
