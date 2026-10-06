package catalog

import (
	"errors"
	"sort"
	"time"
)

var ErrInvalidSnapshot = errors.New("invalid catalog snapshot")

// Catalog is an in-memory read-only view over one validated snapshot.
type Catalog struct {
	snapshot Snapshot
}

// New creates a queryable catalog from canonical pricing records.
func New(version string, records []PricingRecord, generatedAt time.Time) (Catalog, error) {
	if version == "" || generatedAt.IsZero() {
		return Catalog{}, ErrInvalidSnapshot
	}
	if err := ValidateRecords(records); err != nil {
		return Catalog{}, err
	}

	copied := cloneRecords(records)
	SortRecords(copied)

	return Catalog{snapshot: Snapshot{
		Version:     version,
		GeneratedAt: generatedAt,
		Records:     copied,
	}}, nil
}

// FromSnapshot creates a queryable catalog from one validated snapshot.
func FromSnapshot(snapshot Snapshot) (Catalog, error) {
	if snapshot.Version == "" || snapshot.GeneratedAt.IsZero() {
		return Catalog{}, ErrInvalidSnapshot
	}
	if err := ValidateRecords(snapshot.Records); err != nil {
		return Catalog{}, err
	}
	copied := cloneRecords(snapshot.Records)
	SortRecords(copied)
	snapshot.Records = copied
	return Catalog{snapshot: snapshot}, nil
}

// Snapshot returns a copy of the underlying snapshot metadata and records.
func (c Catalog) Snapshot() Snapshot {
	copied := cloneRecords(c.snapshot.Records)
	return Snapshot{
		Version:     c.snapshot.Version,
		GeneratedAt: c.snapshot.GeneratedAt,
		Records:     copied,
	}
}

// PriceQuery selects zero or more records from a catalog.
type PriceQuery struct {
	Provider         string
	Product          string
	SKU              string
	Region           string
	PurchaseModel    string
	BillingUnit      string
	PricingDimension string
}

// Prices returns all records matching the non-empty query fields.
func (c Catalog) Prices(q PriceQuery) []PricingRecord {
	result := make([]PricingRecord, 0)
	for _, record := range c.snapshot.Records {
		if q.Provider != "" && record.Provider != q.Provider {
			continue
		}
		if q.Product != "" && record.Product != q.Product {
			continue
		}
		if q.SKU != "" && record.SKU != q.SKU {
			continue
		}
		if q.Region != "" && record.Region != q.Region {
			continue
		}
		if q.PurchaseModel != "" && record.PurchaseModel != q.PurchaseModel {
			continue
		}
		if q.BillingUnit != "" && record.BillingUnit != q.BillingUnit {
			continue
		}
		if q.PricingDimension != "" && record.PricingDimension != q.PricingDimension {
			continue
		}
		result = append(result, record)
	}
	return result
}

// cloneRecords returns a defensive copy of the canonical records.
func cloneRecords(records []PricingRecord) []PricingRecord {
	copied := make([]PricingRecord, len(records))
	for i, record := range records {
		copied[i] = record
		if record.Dimensions != nil {
			copied[i].Dimensions = cloneMap(record.Dimensions)
		}
		if record.Attributes != nil {
			copied[i].Attributes = cloneMap(record.Attributes)
		}
		if record.Tier != nil {
			tier := *record.Tier
			copied[i].Tier = &tier
		}
		if record.EffectiveFrom != nil {
			t := *record.EffectiveFrom
			copied[i].EffectiveFrom = &t
		}
		if record.EffectiveTo != nil {
			t := *record.EffectiveTo
			copied[i].EffectiveTo = &t
		}
	}
	return copied
}

func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func SortRecords(records []PricingRecord) {
	sort.Slice(records, func(i, j int) bool {
		return RecordKey(records[i]) < RecordKey(records[j])
	})
}
