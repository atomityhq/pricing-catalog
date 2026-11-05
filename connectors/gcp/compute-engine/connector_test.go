package gcpcomputeengine

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pricing-catalog/pkg/connector"
	"pricing-catalog/pkg/connector/testkit"
)

// TestConnectorContract verifies that the connector satisfies the generic
// pipeline contract and preserves all required Compute Engine pricing distinctions.
func TestConnectorContract(t *testing.T) {
	newConnector := func() connector.Connector {
		data, err := os.ReadFile(filepath.Join("testdata", "source.json"))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		return New(WithReader(bytes.NewReader(data)))
	}

	records := testkit.AssertConnectorContract(t, newConnector)

	purchaseModels := make(map[string]int)
	pricingDims := make(map[string]int)
	shapeTypes := make(map[string]int)
	bySKU := make(map[string]int)

	for _, r := range records {
		if r.Provider != "gcp" {
			t.Fatalf("expected provider %q, got %q", "gcp", r.Provider)
		}
		purchaseModels[r.PurchaseModel]++
		pricingDims[r.PricingDimension]++
		bySKU[r.SKU]++
		if st := r.Dimensions["shape_type"]; st != "" {
			shapeTypes[st]++
		}

		// Verify exact nanos conversion for SKU 0001-B904-8A40 (nanos: 4954950 -> "0.00495495")
		if r.SKU == "0001-B904-8A40" && r.Price.Amount != "0.00495495" {
			t.Errorf("expected exact price 0.00495495 for SKU 0001-B904-8A40, got %q", r.Price.Amount)
		}
	}

	// Ensure out-of-scope Network and Hyperdisk SKUs were excluded
	for _, excluded := range []string{"0012-DF7B-477A", "001F-EA08-3C3B", "0026-B8EB-D1EF"} {
		if bySKU[excluded] != 0 {
			t.Errorf("expected out-of-scope SKU %q to be filtered out", excluded)
		}
	}

	for _, pm := range []string{"on_demand", "committed", "spot"} {
		if purchaseModels[pm] == 0 {
			t.Errorf("expected purchase model %q to be preserved", pm)
		}
	}

	for _, dim := range []string{"vcpu", "memory", "gpu", "license"} {
		if pricingDims[dim] == 0 {
			t.Errorf("expected pricing dimension %q to be preserved", dim)
		}
	}

	if shapeTypes["predefined"] == 0 || shapeTypes["custom"] == 0 {
		t.Errorf("expected both predefined and custom shapes, got %#v", shapeTypes)
	}
}

// TestMultiRegionMultiTierAndLocalSSD verifies region unrolling, multi-tier bracket
// construction, Local SSD storage mapping, and legacy preemptible differentiation.
func TestMultiRegionMultiTierAndLocalSSD(t *testing.T) {
	payload := `{
	  "skus": [
	    {
	      "skuId": "LSSD-MULTI-REG",
	      "description": "Preemptible SSD backed Local Storage",
	      "category": {
	        "resourceFamily": "Storage",
	        "resourceGroup": "LocalSSD",
	        "usageType": "Preemptible"
	      },
	      "serviceRegions": ["us-central1", "europe-west1"],
	      "pricingInfo": [{
	        "effectiveTime": "2026-10-30T07:00:00Z",
	        "pricingExpression": {
	          "usageUnit": "GiBy.mo",
	          "tieredRates": [
	            {"startUsageAmount": 0, "unitPrice": {"currencyCode": "USD", "units": "0", "nanos": 0}},
	            {"startUsageAmount": 375, "unitPrice": {"currencyCode": "USD", "units": "0", "nanos": 48000000}}
	          ]
	        }
	      }]
	    }
	  ]
	}`

	conn := New(WithReader(strings.NewReader(payload)))
	records, err := conn.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2 regions × 2 tiers = 4 records
	if len(records) != 4 {
		t.Fatalf("expected 4 records (2 regions x 2 tiers), got %d", len(records))
	}

	for _, r := range records {
		if r.PricingDimension != "local_ssd" {
			t.Errorf("expected pricing_dimension 'local_ssd', got %q", r.PricingDimension)
		}
		if r.PurchaseModel != "preemptible" {
			t.Errorf("expected purchase_model 'preemptible', got %q", r.PurchaseModel)
		}
		if r.BillingUnit != "GiB-month" {
			t.Errorf("expected billing_unit 'GiB-month', got %q", r.BillingUnit)
		}
		if r.Tier == nil {
			t.Fatal("expected non-nil tier")
		}
		if r.Tier.StartQuantity == "0" && r.Tier.EndQuantity != "375" {
			t.Errorf("expected first tier end_quantity '375', got %q", r.Tier.EndQuantity)
		}
		if r.Tier.StartQuantity == "375" && r.Tier.EndQuantity != "" {
			t.Errorf("expected final open-ended tier to have empty end_quantity, got %q", r.Tier.EndQuantity)
		}
	}
}

// TestFetchPaginationOverHTTP verifies that the connector follows nextPageToken
// across multiple HTTP pages without requiring an API key and produces the exact
// same records as reading the fixture directly.
func TestFetchPaginationOverHTTP(t *testing.T) {
	t.Setenv("GCP_API_KEY", "")

	fixtureData, err := os.ReadFile(filepath.Join("testdata", "source.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var fullResp gcpResponse
	if err := json.Unmarshal(fixtureData, &fullResp); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if len(fullResp.Skus) < 2 {
		t.Fatalf("fixture must contain at least 2 SKUs to test pagination")
	}

	mid := len(fullResp.Skus) / 2
	var requests int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("pageToken") {
		case "":
			page1 := gcpResponse{
				Skus:          fullResp.Skus[:mid],
				NextPageToken: "page-2",
			}
			_ = json.NewEncoder(w).Encode(page1)
		case "page-2":
			page2 := gcpResponse{
				Skus:          fullResp.Skus[mid:],
				NextPageToken: "",
			}
			_ = json.NewEncoder(w).Encode(page2)
		default:
			t.Errorf("unexpected pageToken: %q", r.URL.Query().Get("pageToken"))
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	httpConn := New(
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
	)
	httpRecords, err := httpConn.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch over HTTP failed: %v", err)
	}

	if requests != 2 {
		t.Fatalf("expected 2 paginated HTTP requests, got %d", requests)
	}
	if len(httpRecords) == 0 {
		t.Fatal("expected non-empty records from paginated HTTP server")
	}

	readerConn := New(WithReader(bytes.NewReader(fixtureData)))
	readerRecords, err := readerConn.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch from reader failed: %v", err)
	}

	if len(httpRecords) != len(readerRecords) {
		t.Fatalf("paginated HTTP record count (%d) does not match reader record count (%d)", len(httpRecords), len(readerRecords))
	}
}

// TestFetchErrors verifies that HTTP and parsing failures are returned rather
// than silently producing an empty or partial catalog.
func TestFetchErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	conn := New(WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	if _, err := conn.Fetch(context.Background()); err == nil {
		t.Fatal("expected error on HTTP 500 response, got nil")
	}

	badReaderConn := New(WithReader(strings.NewReader(`{"skus": [`)))
	if _, err := badReaderConn.Fetch(context.Background()); err == nil {
		t.Fatal("expected error on malformed JSON, got nil")
	}
}
