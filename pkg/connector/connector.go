package connector

import (
	"context"

	"pricing-catalog/pkg/catalog"
)

// Connector converts one provider's pricing source into canonical records.
// Provider-specific parsing and normalization should remain inside the
// connector package rather than leaking into the generic catalog API.
type Connector interface {
	Name() string
	Fetch(ctx context.Context) ([]catalog.PricingRecord, error)
}
