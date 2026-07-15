package operation

import (
	"errors"
	"testing"
)

type resultStub struct {
	rows int64
	err  error
}

func (r resultStub) LastInsertId() (int64, error) { return 0, nil }

func (r resultStub) RowsAffected() (int64, error) {
	return r.rows, r.err
}

func TestRequireOneRow(t *testing.T) {
	t.Parallel()

	if err := requireOneRow(resultStub{rows: 1}); err != nil {
		t.Fatalf("requireOneRow() error = %v", err)
	}
	if err := requireOneRow(resultStub{rows: 0}); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("requireOneRow() error = %v; want ErrLeaseLost", err)
	}
}

func TestNullJSON(t *testing.T) {
	t.Parallel()

	if value := nullJSON(nil); value != nil {
		t.Fatalf("nullJSON(nil) = %#v", value)
	}
	if value := nullJSON([]byte(`{"ok":true}`)); value == nil {
		t.Fatal("nullJSON(non-empty) = nil")
	}
}
