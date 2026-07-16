package order

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Aggregate is one per-product, per-instance capacity-window summary.
type Aggregate struct {
	ProductID      uint64
	InstanceID     string
	TimeBucket     time.Time
	ReceivedUnits  int
	ProcessedUnits int
	DroppedUnits   int
}

// Repository stores bounded aggregate order statistics.
type Repository struct {
	database *sql.DB
}

// NewRepository returns an order aggregate repository.
func NewRepository(database *sql.DB) *Repository {
	return &Repository{database: database}
}

// Add increments one aggregate bucket without storing individual orders.
func (r *Repository) Add(ctx context.Context, value Aggregate) error {
	_, err := r.database.ExecContext(ctx, `
		INSERT INTO order_stats (
			product_id, instance_name, time_bucket,
			received_orders, processed_orders, dropped_orders
		) VALUES (?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			received_orders = received_orders + VALUES(received_orders),
			processed_orders = processed_orders + VALUES(processed_orders),
			dropped_orders = dropped_orders + VALUES(dropped_orders)`,
		value.ProductID,
		value.InstanceID,
		value.TimeBucket,
		value.ReceivedUnits,
		value.ProcessedUnits,
		value.DroppedUnits,
	)
	if err != nil {
		return fmt.Errorf("add order aggregate: %w", err)
	}
	return nil
}
