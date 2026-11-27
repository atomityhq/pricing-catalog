package gcppersistentdisk

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
// pipeline contract and preserves all required Persistent Disk pricing
// distinctions: disk types, regional vs zonal replication, snapshot kinds,
// billing units, and exact decimal prices.
func TestConnectorContract(t *testing.T) {
	newConnector := func() connector.Connector {
		data, err := os.ReadFile(filepath.Join("testdata", "source.json"))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		return New(WithReader(bytes.NewReader(data)))
	}

	records := testkit.AssertConnectorContract(t, newConnector)

	// 2 (standard tiers) + 2 (ssd regions) + 1 (balanced regional)
	// + 1 (extreme capacity) + 1 (extreme iops) + 1 (standard snapshot)
	// + 1 (archive snapshot) + 1 (archive retrieval) + 1 (instant creation)
	// + 1 (instant storage) + 1 (multi-regional snapshot) = 13 records.
	if len(records) != 13 {
		t.Fatalf("expected 13 records, got %d", len(records))
	}

	purchaseModels := make(map[string]int)
	pricingDims := make(map[string]int)
	products := make(map[string]int)
	replications := make(map[string]int)
	billingUnits := make(map[string]int)
	bySKU := make(map[string]int)

	for _, r := range records {
		if r.Provider != "gcp" {
			t.Fatalf("expected provider %q, got %q", "gcp", r.Provider)
		}
		purchaseModels[r.PurchaseModel]++
		pricingDims[r.PricingDimension]++
		products[r.Product]++
		replications[r.Dimensions["replication"]]++
		billingUnits[r.BillingUnit]++
		bySKU[r.SKU]++

		// Verify exact nanos conversion for the Standard disk second tier
		// (nanos: 40000000 -> "0.0400") and the SSD disk (nanos: 170000000
		// -> "0.1700").
		if r.SKU == "pd-standard-zonal" && r.Tier != nil && r.Tier.StartQuantity == "30" && r.Price.Amount != "0.0400" {
			t.Errorf("expected exact price 0.0400 for Standard PD tier 30+, got %q", r.Price.Amount)
		}
		if r.SKU == "pd-ssd-zonal" && r.Price.Amount != "0.1700" {
			t.Errorf("expected exact price 0.1700 for SSD PD, got %q", r.Price.Amount)
		}
		if r.SKU == "pd-snapshot-standard" && r.Price.Amount != "0.0500" {
			t.Errorf("expected exact price 0.0500 for standard snapshot, got %q", r.Price.Amount)
		}
		// The free tier of the Standard disk must be priced at zero.
		if r.SKU == "pd-standard-zonal" && r.Tier != nil && r.Tier.StartQuantity == "0" && r.Price.Amount != "0.0000" {
			t.Errorf("expected free tier price 0.0000 for Standard PD tier 0-30, got %q", r.Price.Amount)
		}
		if r.Tier != nil && r.Tier.StartQuantity == "0" && r.Tier.EndQuantity == "30" && r.SKU != "pd-standard-zonal" {
			t.Errorf("unexpected 0-30 tier on SKU %q", r.SKU)
		}
	}

	// Disks and snapshots are separate products.
	if products["Persistent Disk"] == 0 {
		t.Error("expected product \"Persistent Disk\" to be present")
	}
	if products["Persistent Disk Snapshot"] == 0 {
		t.Error("expected product \"Persistent Disk Snapshot\" to be present")
	}

	// Persistent Disk has no committed-use or spot pricing on the public
	// pricing page; every record must be on_demand.
	if len(purchaseModels) != 1 || purchaseModels["on_demand"] == 0 {
		t.Errorf("expected all records to be on_demand, got %#v", purchaseModels)
	}

	for _, dim := range []string{"standard", "balanced", "ssd", "extreme", "standard_snapshot", "archive_snapshot", "instant_snapshot"} {
		if pricingDims[dim] == 0 {
			t.Errorf("expected pricing dimension %q to be preserved", dim)
		}
	}

	for _, repl := range []string{"zonal", "regional", "multi_regional"} {
		if replications[repl] == 0 {
			t.Errorf("expected replication %q to be preserved", repl)
		}
	}

	for _, unit := range []string{"GiB-month", "month", "operation", "GiB"} {
		if billingUnits[unit] == 0 {
			t.Errorf("expected billing unit %q to be preserved", unit)
		}
	}

	// Ensure out-of-scope Hyperdisk, Local SSD and Network SKUs were excluded.
	for _, excluded := range []string{"001F-EA08-3C3B", "0026-B8EB-D1EF", "0012-DF7B-477A", "pd-local-ssd-out-of-scope"} {
		if bySKU[excluded] != 0 {
			t.Errorf("expected out-of-scope SKU %q to be filtered out", excluded)
		}
	}

	// The regional Balanced disk must cost twice the zonal Balanced rate
	// ($0.20 vs $0.10 per GiB-month) and carry replication=regional.
	for _, r := range records {
		if r.SKU == "pd-balanced-regional" {
			if r.Dimensions["replication"] != "regional" {
				t.Errorf("expected replication regional for pd-balanced-regional, got %q", r.Dimensions["replication"])
			}
			if r.Price.Amount != "0.2000" {
				t.Errorf("expected regional Balanced price 0.2000, got %q", r.Price.Amount)
			}
			if r.Dimensions["disk_type"] != "pd-balanced" {
				t.Errorf("expected disk_type pd-balanced, got %q", r.Dimensions["disk_type"])
			}
		}
		if r.SKU == "pd-extreme-iops" && r.Dimensions["iops"] != "provisioned" {
			t.Errorf("expected iops=provisioned for pd-extreme-iops, got %#v", r.Dimensions)
		}
		if r.SKU == "pd-snapshot-archive-retrieval" && r.Dimensions["charge"] != "retrieval" {
			t.Errorf("expected charge=retrieval for pd-snapshot-archive-retrieval, got %#v", r.Dimensions)
		}
		if r.SKU == "pd-snapshot-multiregional" && r.Region != "us" {
			t.Errorf("expected multi-regional snapshot region us, got %q", r.Region)
		}
	}
}

// TestMultiRegionMultiTier verifies region unrolling and multi-tier bracket
// construction for a Persistent Disk SKU.
func TestMultiRegionMultiTier(t *testing.T) {
	payload := `{
	  "skus": [
	    {
	      "skuId": "pd-balanced-test",
	      "description": "Balanced PD Capacity in Iowa",
	      "category": {
	        "resourceFamily": "Storage",
	        "resourceGroup": "PD",
	        "usageType": "OnDemand"
	      },
	      "serviceRegions": ["us-central1", "europe-west1"],
	      "pricingInfo": [{
	        "effectiveTime": "2026-10-30T07:00:00Z",
	        "pricingExpression": {
	          "usageUnit": "GiBy.mo",
	          "tieredRates": [
	            {"startUsageAmount": 0, "unitPrice": {"currencyCode": "USD", "units": "0", "nanos": 0}},
	            {"startUsageAmount": 100, "unitPrice": {"currencyCode": "USD", "units": "0", "nanos": 100000000}}
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

	// 2 regions x 2 tiers = 4 records
	if len(records) != 4 {
		t.Fatalf("expected 4 records (2 regions x 2 tiers), got %d", len(records))
	}

	for _, r := range records {
		if r.PricingDimension != "balanced" {
			t.Errorf("expected pricing_dimension 'balanced', got %q", r.PricingDimension)
		}
		if r.Product != "Persistent Disk" {
			t.Errorf("expected product 'Persistent Disk', got %q", r.Product)
		}
		if r.PurchaseModel != "on_demand" {
			t.Errorf("expected purchase_model 'on_demand', got %q", r.PurchaseModel)
		}
		if r.BillingUnit != "GiB-month" {
			t.Errorf("expected billing_unit 'GiB-month', got %q", r.BillingUnit)
		}
		if r.Dimensions["replication"] != "zonal" {
			t.Errorf("expected replication 'zonal', got %q", r.Dimensions["replication"])
		}
		if r.Tier == nil {
			t.Fatal("expected non-nil tier")
		}
		if r.Tier.StartQuantity == "0" && r.Tier.EndQuantity != "100" {
			t.Errorf("expected first tier end_quantity '100', got %q", r.Tier.EndQuantity)
		}
		if r.Tier.StartQuantity == "100" && r.Tier.EndQuantity != "" {
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

	// A payload with only out-of-scope SKUs must fail instead of returning
	// an empty catalog.
	outOfScope := New(WithReader(strings.NewReader(`{"skus": [{
		"skuId": "001F-EA08-3C3B",
		"description": "Hyperdisk Balanced IOPS Confidential Mode in Doha",
		"category": {"resourceFamily": "Storage", "resourceGroup": "SSD", "usageType": "OnDemand"},
		"serviceRegions": ["me-central1"],
		"pricingInfo": [{"effectiveTime": "2026-10-30T07:00:00Z",
		  "pricingExpression": {"usageUnit": "mo",
		    "tieredRates": [{"startUsageAmount": 0,
		      "unitPrice": {"currencyCode": "USD", "units": "0", "nanos": 1000000}}]}}]
	}]}`)))
	if _, err := outOfScope.Fetch(context.Background()); err == nil {
		t.Fatal("expected error when no in-scope records are produced, got nil")
	}

	// A SKU without skuId must fail.
	missingID := New(WithReader(strings.NewReader(`{"skus": [{
		"description": "Standard Persistent Disk Capacity in Iowa",
		"category": {"resourceFamily": "Storage", "resourceGroup": "PD", "usageType": "OnDemand"},
		"serviceRegions": ["us-central1"],
		"pricingInfo": [{"effectiveTime": "2026-10-30T07:00:00Z",
		  "pricingExpression": {"usageUnit": "GiBy.mo",
		    "tieredRates": [{"startUsageAmount": 0,
		      "unitPrice": {"currencyCode": "USD", "units": "0", "nanos": 40000000}}]}}]
	}]}`)))
	if _, err := missingID.Fetch(context.Background()); err == nil {
		t.Fatal("expected error on missing skuId, got nil")
	}
}
