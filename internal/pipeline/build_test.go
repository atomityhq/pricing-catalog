package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pricing-catalog/pkg/catalog"
)

type fakeConnector struct {
	name    string
	records []catalog.PricingRecord
	err     error
}

func (f fakeConnector) Name() string {
	return f.name
}

func (f fakeConnector) Fetch(context.Context) ([]catalog.PricingRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.records, nil
}

func TestBuildSnapshotValidatesAndSorts(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	recordA := validRecord("a")
	recordB := validRecord("b")

	snapshot, err := BuildSnapshot(
		context.Background(),
		fakeConnector{
			name:    "test-provider",
			records: []catalog.PricingRecord{recordB, recordA},
		},
		BuildOptions{
			Version:     "test-1",
			GeneratedAt: now,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Version != "test-1" {
		t.Fatalf("unexpected version: %q", snapshot.Version)
	}

	if !snapshot.GeneratedAt.Equal(now) {
		t.Fatalf("unexpected generated_at: %v", snapshot.GeneratedAt)
	}

	if len(snapshot.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(snapshot.Records))
	}

	if snapshot.Records[0].SKU != "a" || snapshot.Records[1].SKU != "b" {
		t.Fatalf("records were not deterministically sorted: %#v", snapshot.Records)
	}
}

func TestBuildSnapshotRejectsEmptyConnectorResult(t *testing.T) {
	_, err := BuildSnapshot(
		context.Background(),
		fakeConnector{name: "empty"},
		BuildOptions{
			Version:     "test-1",
			GeneratedAt: time.Unix(0, 0).UTC(),
		},
	)
	if !errors.Is(err, ErrEmptyCatalog) {
		t.Fatalf("expected ErrEmptyCatalog, got %v", err)
	}
}

func TestBuildSnapshotRejectsInvalidRecords(t *testing.T) {
	record := validRecord("a")
	record.Price.Amount = "not-a-price"

	_, err := BuildSnapshot(
		context.Background(),
		fakeConnector{
			name:    "invalid",
			records: []catalog.PricingRecord{record},
		},
		BuildOptions{
			Version:     "test-1",
			GeneratedAt: time.Unix(0, 0).UTC(),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "price.amount") {
		t.Fatalf("expected price validation error, got %v", err)
	}
}

func TestBuildSnapshotPropagatesConnectorErrors(t *testing.T) {
	expected := errors.New("provider unavailable")

	_, err := BuildSnapshot(
		context.Background(),
		fakeConnector{
			name: "failing",
			err:  expected,
		},
		BuildOptions{
			Version:     "test-1",
			GeneratedAt: time.Unix(0, 0).UTC(),
		},
	)
	if !errors.Is(err, expected) {
		t.Fatalf("expected connector error, got %v", err)
	}
}

func TestWriteSnapshotUsesValidatedSortedOutput(t *testing.T) {
	var out strings.Builder

	err := BuildAndWriteJSON(
		context.Background(),
		fakeConnector{
			name:    "test-provider",
			records: []catalog.PricingRecord{validRecord("b"), validRecord("a")},
		},
		BuildOptions{
			Version:     "test-1",
			GeneratedAt: time.Unix(0, 0).UTC(),
		},
		&out,
	)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := catalog.LoadJSON(strings.NewReader(out.String()))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Records[0].SKU != "a" || snapshot.Records[1].SKU != "b" {
		t.Fatalf("unexpected snapshot ordering: %#v", snapshot.Records)
	}
}

func validRecord(sku string) catalog.PricingRecord {
	return catalog.PricingRecord{
		Provider:         "test-provider",
		Product:          "VM",
		SKU:              sku,
		Region:           "eu-west",
		PurchaseModel:    "on_demand",
		BillingUnit:      "hour",
		PricingDimension: "unit",
		Price: catalog.Money{
			Amount:   "0.10",
			Currency: "EUR",
		},
		Source: catalog.Source{
			Type: "fixture",
			URL:  "https://example.invalid/pricing",
		},
	}
}
