package mysql

import (
	"context"
	"database/sql"
	"errors"

	"website-gobased/internal/domain"
	"website-gobased/internal/repository"
)

type PurchaseRepository struct {
	db *sql.DB
}

func NewPurchaseRepository(db *sql.DB) *PurchaseRepository {
	return &PurchaseRepository{db: db}
}

func (r *PurchaseRepository) Create(
	ctx context.Context,
	input repository.CreatePurchaseInput,
) (domain.Purchase, error) {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO purchases
			(user_id, product_id, quantity, total_amount, currency, status)
		VALUES (?, ?, ?, ?, ?, ?)`,
		input.UserID,
		input.ProductID,
		input.Quantity,
		input.TotalAmount,
		input.Currency,
		input.Status,
	)
	if err != nil {
		return domain.Purchase{}, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return domain.Purchase{}, err
	}

	return r.GetByID(ctx, id)
}

func (r *PurchaseRepository) GetByID(
	ctx context.Context,
	id int64,
) (domain.Purchase, error) {
	var purchase domain.Purchase

	err := r.db.QueryRowContext(
		ctx,
		`SELECT
			id, user_id, product_id, quantity, total_amount,
			currency, status, created_at, updated_at
		FROM purchases
		WHERE id = ?`,
		id,
	).Scan(
		&purchase.ID,
		&purchase.UserID,
		&purchase.ProductID,
		&purchase.Quantity,
		&purchase.TotalAmount,
		&purchase.Currency,
		&purchase.Status,
		&purchase.CreatedAt,
		&purchase.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Purchase{}, repository.ErrPurchaseNotFound
	}
	if err != nil {
		return domain.Purchase{}, err
	}

	return purchase, nil
}

func (r *PurchaseRepository) List(
	ctx context.Context,
) ([]domain.Purchase, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT
			id, user_id, product_id, quantity, total_amount,
			currency, status, created_at, updated_at
		FROM purchases
		ORDER BY id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	purchases := make([]domain.Purchase, 0)
	for rows.Next() {
		var purchase domain.Purchase
		if err := rows.Scan(
			&purchase.ID,
			&purchase.UserID,
			&purchase.ProductID,
			&purchase.Quantity,
			&purchase.TotalAmount,
			&purchase.Currency,
			&purchase.Status,
			&purchase.CreatedAt,
			&purchase.UpdatedAt,
		); err != nil {
			return nil, err
		}
		purchases = append(purchases, purchase)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return purchases, nil
}

func (r *PurchaseRepository) UpdateStatus(
	ctx context.Context,
	id int64,
	status string,
) (domain.Purchase, error) {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE purchases
		SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		status,
		id,
	)
	if err != nil {
		return domain.Purchase{}, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return domain.Purchase{}, err
	}
	if rowsAffected == 0 {
		return domain.Purchase{}, repository.ErrPurchaseNotFound
	}

	return r.GetByID(ctx, id)
}

func (r *PurchaseRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(
		ctx,
		`DELETE FROM purchases WHERE id = ?`,
		id,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repository.ErrPurchaseNotFound
	}

	return nil
}
