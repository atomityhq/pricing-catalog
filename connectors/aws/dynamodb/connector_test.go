package dynamodb

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
	"pricing-catalog/pkg/connector/testkit"
)

func loadFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "source.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func newFixtureConnector(t *testing.T) *Connector {
	t.Helper()
	return New(WithReader(bytes.NewReader(loadFixture(t))))
}

func TestConnectorContract(t *testing.T) {
	records := testkit.AssertConnectorContract(t, func() connector.Connector {
		return newFixtureConnector(t)
	})
	if len(records) == 0 {
		t.Fatal("expected non-empty records")
	}
}

func TestFetchNormalizesOnDemandAndReservedPricing(t *testing.T) {
	records, err := newFixtureConnector(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	foundStorage, foundCDC, foundGlobal := false, false, false
	foundHourly, foundUpfront := false, false
	for _, record := range records {
		switch record.SKU {
		case "SKU-STORAGE":
			foundStorage = record.PricingDimension == "pitr_backup_storage" && record.BillingUnit == "GB-Mo" && record.Price.Amount == "0.20"
		case "SKU-CDC":
			foundCDC = record.PricingDimension == "change_data_capture" && record.Dimensions["group"] == "DDB-Kinesis"
		case "SKU-GLOBAL":
			foundGlobal = record.Region == "global" && record.PricingDimension == "vector_search"
		case "SKU-RESERVED":
			if record.PurchaseModel == "reserved" && record.Attributes["rate_code"] == "RATE-RES-HOURLY" {
				foundHourly = record.PricingDimension == "read_capacity" && record.BillingUnit == "ReadCapacityUnit-Hrs"
			}
			if record.PurchaseModel == "reserved" && record.Attributes["rate_code"] == "RATE-RES-UPFRONT" {
				foundUpfront = record.PricingDimension == "upfront_fee" && record.BillingUnit == "Quantity" && record.Price.Amount == "12.00"
			}
		case "SKU-TEST":
			t.Errorf("test SKU should be excluded")
		}
	}
	if !foundStorage || !foundCDC || !foundGlobal || !foundHourly || !foundUpfront {
		t.Fatalf("missing expected normalized records: storage=%v cdc=%v global=%v hourly=%v upfront=%v", foundStorage, foundCDC, foundGlobal, foundHourly, foundUpfront)
	}
}

func TestFetchPreservesTierBoundariesAndFreePrices(t *testing.T) {
	input := `{"products":{"SKU-TIER":{"sku":"SKU-TIER","productFamily":"Amazon DynamoDB PayPerRequest Throughput","attributes":{"regionCode":"us-east-1","group":"DDB-ReadUnits","usagetype":"USE1-ReadRequestUnits"}}},"terms":{"OnDemand":{"SKU-TIER":{"TERM-TIER":{"offerTermCode":"OD","sku":"SKU-TIER","effectiveDate":"2026-11-01T00:00:00Z","priceDimensions":{"RATE-FREE":{"rateCode":"RATE-FREE","description":"Free tier","unit":"ReadRequestUnits","beginRange":"0","endRange":"2500000","pricePerUnit":{"USD":"0.00"}},"RATE-PAID":{"rateCode":"RATE-PAID","description":"Paid tier","unit":"ReadRequestUnits","beginRange":"2500000","endRange":"Inf","pricePerUnit":{"USD":"0.000001"}}},"termAttributes":{}}}}}}`
	records, err := New(WithReader(strings.NewReader(input))).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	for _, record := range records {
		if record.Price.Amount == "0.00" && (record.Tier == nil || record.Tier.EndQuantity != "2500000") {
			t.Errorf("free tier boundary not preserved: %+v", record)
		}
		if record.Price.Amount == "0.000001" && (record.Tier == nil || record.Tier.StartQuantity != "2500000" || record.Tier.EndQuantity != "") {
			t.Errorf("open paid tier not preserved: %+v", record)
		}
	}
}

func TestFetchUsesHTTPSourceAndRejectsNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %s, want GET", r.Method)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := New(WithSourceURL(server.URL)).Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected HTTP status 503") {
		t.Fatalf("Fetch() error = %v, want HTTP 503 error", err)
	}
	var sourceErr *connector.SourceError
	if !errors.As(err, &sourceErr) {
		t.Fatalf("error type = %T, want *connector.SourceError", err)
	}
}

func TestFetchReturnsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newFixtureConnector(t).Fetch(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch() error = %v, want context.Canceled", err)
	}
}

func TestFetchOutputPassesCatalogValidation(t *testing.T) {
	records, err := newFixtureConnector(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if err := catalog.ValidateRecords(records); err != nil {
		t.Fatalf("ValidateRecords() error = %v", err)
	}
}
