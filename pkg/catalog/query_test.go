package catalog

import (
	"testing"
	"time"
)

func TestCatalogPricesFiltersAndSorts(t *testing.T) {
	records := []PricingRecord{
		{
			Provider: "provider-a", Product: "vm", SKU: "b", Region: "eu", PurchaseModel: "on_demand",
			BillingUnit: "hour", PricingDimension: "unit", Price: Money{Amount: "0.20", Currency: "EUR"},
			Source: Source{Type: "fixture", URL: "https://example.invalid/p"},
		},
		{
			Provider: "provider-a", Product: "vm", SKU: "a", Region: "eu", PurchaseModel: "on_demand",
			BillingUnit: "hour", PricingDimension: "unit", Price: Money{Amount: "0.10", Currency: "EUR"},
			Source: Source{Type: "fixture", URL: "https://example.invalid/p"},
		},
	}

	c, err := New("test", records, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}

	got := c.Prices(PriceQuery{SKU: "a"})
	if len(got) != 1 || got[0].SKU != "a" {
		t.Fatalf("unexpected query result: %#v", got)
	}

	all := c.Snapshot().Records
	if len(all) != 2 || all[0].SKU != "a" || all[1].SKU != "b" {
		t.Fatalf("records are not deterministic: %#v", all)
	}
}

func TestCatalogSnapshotIsIndependent(t *testing.T) {
	record := validTestRecord()
	record.Dimensions = map[string]string{"vcpus": "2"}
	c, err := New("test", []PricingRecord{record}, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}

	snapshot := c.Snapshot()
	snapshot.Records[0].Dimensions["vcpus"] = "99"
	if c.Snapshot().Records[0].Dimensions["vcpus"] != "2" {
		t.Fatal("snapshot exposed mutable internal dimensions")
	}
}

func validTestRecord() PricingRecord {
	return PricingRecord{
		Provider: "provider-a", Product: "vm", SKU: "a", Region: "eu", PurchaseModel: "on_demand",
		BillingUnit: "hour", PricingDimension: "unit", Price: Money{Amount: "0.10", Currency: "EUR"},
		Source: Source{Type: "fixture", URL: "https://example.invalid/p"},
	}
}

func TestValidateRecordsRejectsDuplicateKeys(t *testing.T) {
	record := validTestRecord()

	if err := ValidateRecords([]PricingRecord{record, record}); err == nil {
		t.Fatal("expected duplicate key validation error")
	}
}

func TestLoadEmbeddedExample(t *testing.T) {
	c, err := LoadEmbeddedExample()
	if err != nil {
		t.Fatal(err)
	}

	if got := len(c.Prices(PriceQuery{Provider: "example-cloud"})); got != 1 {
		t.Fatalf("expected 1 embedded record, got %d", got)
	}
}

func TestNewRejectsZeroGeneratedAt(t *testing.T) {
	record := PricingRecord{
		Provider:         "provider-a",
		Product:          "vm",
		SKU:              "a",
		Region:           "eu",
		PurchaseModel:    "on_demand",
		BillingUnit:      "hour",
		PricingDimension: "unit",
		Price:            Money{Amount: "0.10", Currency: "EUR"},
		Source:           Source{Type: "fixture", URL: "https://example.invalid/p"},
	}

	if _, err := New("test", []PricingRecord{record}, time.Time{}); err != ErrInvalidSnapshot {
		t.Fatalf("expected ErrInvalidSnapshot, got %v", err)
	}
}
