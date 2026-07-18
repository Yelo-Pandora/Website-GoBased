// Package product reads the isolated lab product catalog.
package product

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"website-gobased/internal/protocol"
)

// ErrNotFound indicates that a requested product does not exist.
var ErrNotFound = errors.New("product not found")

// Repository reads products from one lab database.
type Repository struct {
	database *sql.DB
}

// Get returns one complete cacheable product.
func (r *Repository) Get(ctx context.Context, productID uint64) (protocol.CacheProduct, error) {
	if productID == 0 {
		return protocol.CacheProduct{}, ErrNotFound
	}
	var value protocol.CacheProduct
	err := r.database.QueryRowContext(
		ctx,
		"SELECT id, name, category, version FROM products WHERE id = ?",
		productID,
	).Scan(&value.ID, &value.Name, &value.Category, &value.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.CacheProduct{}, ErrNotFound
	}
	if err != nil {
		return protocol.CacheProduct{}, fmt.Errorf("read product: %w", err)
	}
	return value, nil
}

// List returns the bounded product catalog in stable ID order.
func (r *Repository) List(ctx context.Context) ([]protocol.CacheProduct, error) {
	rows, err := r.database.QueryContext(ctx, "SELECT id, name, category, version FROM products ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()
	values := make([]protocol.CacheProduct, 0, 12)
	for rows.Next() {
		var value protocol.CacheProduct
		if err := rows.Scan(&value.ID, &value.Name, &value.Category, &value.Version); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate products: %w", err)
	}
	return values, nil
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
