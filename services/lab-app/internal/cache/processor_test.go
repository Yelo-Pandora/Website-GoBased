package cache

import (
	"context"
	"testing"
	"time"

	"website-gobased/internal/protocol"
)

type productRepositoryStub struct {
	products []protocol.CacheProduct
}

func (s productRepositoryStub) Get(_ context.Context, productID uint64) (protocol.CacheProduct, error) {
	for _, productValue := range s.products {
		if productValue.ID == productID {
			return productValue, nil
		}
	}
	return protocol.CacheProduct{}, nil
}

func (s productRepositoryStub) List(context.Context) ([]protocol.CacheProduct, error) {
	return append([]protocol.CacheProduct(nil), s.products...), nil
}

func TestProcessorL1ControlClearsAndReenablesEmptyStore(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	processor := &Processor{
		products:  productRepositoryStub{products: []protocol.CacheProduct{cacheProduct(1)}},
		l1:        NewL1Store(9, 15*time.Second),
		config:    Config{InstanceID: "app-2"},
		now:       func() time.Time { return now },
		l1Enabled: true,
	}
	processor.l1.Put(cacheProduct(1), now)

	disabled := processor.SetL1Enabled(false)
	if disabled.Status != "disabled" || len(disabled.Entries) != 0 {
		t.Fatalf("disabled state = %#v", disabled)
	}
	enabled := processor.SetL1Enabled(true)
	if enabled.Status != "live" || len(enabled.Entries) != 0 {
		t.Fatalf("enabled state = %#v", enabled)
	}
	state, err := processor.InstanceState(context.Background())
	if err != nil || len(state.Instances) != 1 || len(state.Instances[0].Entries) != 0 {
		t.Fatalf("InstanceState() = %#v, %v", state, err)
	}
}
