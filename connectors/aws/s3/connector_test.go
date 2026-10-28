package s3

import (
	"bytes"
	"context"
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
	data, err := os.ReadFile(filepath.Join("testdata", "pricing.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	return data
}

func newFixtureConnector(t *testing.T) func() connector.Connector {
	data := loadFixture(t)
	return func() connector.Connector {
		return New(bytes.NewReader(data))
	}
}

func TestConnectorContract(t *testing.T) {
	records := testkit.AssertConnectorContract(t, newFixtureConnector(t))

	if len(records) == 0 {
		t.Fatal("expected records from connector, got 0")
	}

	for _, r := range records {
		if r.Provider != "aws" {
			t.Errorf("expected provider 'aws', got %q", r.Provider)
		}
		if r.Product != "Amazon S3" {
			t.Errorf("expected product 'Amazon S3', got %q", r.Product)
		}
		if r.Region != "us-east-1" {
			t.Errorf("expected region 'us-east-1', got %q", r.Region)
		}
		if r.PurchaseModel != "on_demand" {
			t.Errorf("expected purchase model 'on_demand', got %q", r.PurchaseModel)
		}
		if r.Price.Amount == "" || r.Price.Currency == "" {
			t.Errorf("record %s has incomplete price: %+v", r.SKU, r.Price)
		}
	}
}

func TestStorageClasses(t *testing.T) {
	records, err := New(bytes.NewReader(loadFixture(t))).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	expectedClasses := map[string]bool{
		"Standard":                   false,
		"Standard-IA":                false,
		"One Zone-IA":                false,
		"Glacier Instant Retrieval":  false,
		"Glacier Flexible Retrieval": false,
		"Glacier Deep Archive":       false,
		"Express One Zone":           false,
		"Intelligent-Tiering":        false,
	}

	for _, r := range records {
		if r.PricingDimension == "storage" {
			if sc, ok := r.Dimensions["storage_class"]; ok {
				if _, expected := expectedClasses[sc]; expected {
					expectedClasses[sc] = true
				}
			}
		}
	}

	for class, found := range expectedClasses {
		if !found {
			t.Errorf("storage class %q was not found in parsed storage records", class)
		}
	}
}

func TestTieredCapacityPricing(t *testing.T) {
	records, err := New(bytes.NewReader(loadFixture(t))).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	// S3 Standard capacity has SKU WP9ANXZGBYYSGJEA and 3 tiers:
	// Tier 1: 0 to 51200 (50 TB) -> $0.023
	// Tier 2: 51200 to 512000 (450 TB) -> $0.022
	// Tier 3: 512000 to Inf (>500 TB) -> $0.021
	var standardTiers []catalog.PricingRecord
	for _, r := range records {
		if r.SKU == "WP9ANXZGBYYSGJEA" {
			standardTiers = append(standardTiers, r)
		}
	}

	if len(standardTiers) != 3 {
		t.Fatalf("expected 3 tiers for S3 Standard storage, got %d", len(standardTiers))
	}

	// Verify each tier's boundaries and price
	tier1Found := false
	tier2Found := false
	tier3Found := false

	for _, r := range standardTiers {
		if r.Tier == nil {
			t.Fatalf("record %s missing Tier struct", r.ProviderSKUID)
		}

		switch {
		case r.Tier.StartQuantity == "0" && r.Tier.EndQuantity == "51200":
			tier1Found = true
			if r.Price.Amount != "0.0230000000" {
				t.Errorf("tier 1 expected price 0.0230000000, got %s", r.Price.Amount)
			}
		case r.Tier.StartQuantity == "51200" && r.Tier.EndQuantity == "512000":
			tier2Found = true
			if r.Price.Amount != "0.0220000000" {
				t.Errorf("tier 2 expected price 0.0220000000, got %s", r.Price.Amount)
			}
		case r.Tier.StartQuantity == "512000" && r.Tier.EndQuantity == "":
			tier3Found = true
			if r.Price.Amount != "0.0210000000" {
				t.Errorf("tier 3 expected price 0.0210000000, got %s", r.Price.Amount)
			}
		}
	}

	if !tier1Found || !tier2Found || !tier3Found {
		t.Errorf("not all S3 standard tiers found: tier1=%v, tier2=%v, tier3=%v", tier1Found, tier2Found, tier3Found)
	}
}

func TestRequestPricing(t *testing.T) {
	records, err := New(bytes.NewReader(loadFixture(t))).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	var foundTier1, foundTier2, foundTier4 bool

	for _, r := range records {
		if r.PricingDimension == "request" {
			if r.SKU == "E9YHNFENF4XQBZR6" { // Standard Requests-Tier1
				foundTier1 = true
				if r.BillingUnit != "Requests" {
					t.Errorf("expected billing unit 'Requests', got %s", r.BillingUnit)
				}
				if r.Dimensions["tier_type"] != "Tier1" {
					t.Errorf("expected tier_type 'Tier1', got %s", r.Dimensions["tier_type"])
				}
			}
			if r.SKU == "ZWQ6Q48CRJXX4FXE" { // Standard Requests-Tier2
				foundTier2 = true
				if r.Dimensions["tier_type"] != "Tier2" {
					t.Errorf("expected tier_type 'Tier2', got %s", r.Dimensions["tier_type"])
				}
			}
			if r.SKU == "7Q8ZATUMRNWEPNWW" { // Lifecycle Transitions (Requests-Tier4)
				foundTier4 = true
				if r.Dimensions["tier_type"] != "Tier4" {
					t.Errorf("expected tier_type 'Tier4', got %s", r.Dimensions["tier_type"])
				}
			}
		}
	}

	if !foundTier1 {
		t.Error("Tier 1 PUT/POST request pricing was not preserved")
	}
	if !foundTier2 {
		t.Error("Tier 2 GET request pricing was not preserved")
	}
	if !foundTier4 {
		t.Error("Tier 4 lifecycle transition request pricing was not preserved")
	}
}

func TestRetrievalPricing(t *testing.T) {
	records, err := New(bytes.NewReader(loadFixture(t))).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	var siaRetrieval, ziaRetrieval, girRetrieval, glacierBulk bool

	for _, r := range records {
		if r.PricingDimension == "retrieval" {
			switch r.SKU {
			case "7594RTQEE8ND34GA":
				siaRetrieval = true
				if r.Price.Amount != "0.0100000000" {
					t.Errorf("expected SIA retrieval price 0.01, got %s", r.Price.Amount)
				}
				if r.BillingUnit != "GB" {
					t.Errorf("expected billing unit 'GB', got %s", r.BillingUnit)
				}
			case "2C7JEQ54QWYYWGZ6":
				ziaRetrieval = true
			case "CN3DWKRWAMMHWCA3":
				girRetrieval = true
				if r.Price.Amount != "0.0300000000" {
					t.Errorf("expected GIR retrieval price 0.03, got %s", r.Price.Amount)
				}
			case "DJVU2QGWTTYMPVET":
				glacierBulk = true
			}
		}
	}

	if !siaRetrieval {
		t.Error("Standard-IA retrieval was not preserved")
	}
	if !ziaRetrieval {
		t.Error("One Zone-IA retrieval was not preserved")
	}
	if !girRetrieval {
		t.Error("Glacier Instant Retrieval was not preserved")
	}
	if !glacierBulk {
		t.Error("Glacier Bulk retrieval was not preserved")
	}
}

func TestEarlyDeletionFees(t *testing.T) {
	records, err := New(bytes.NewReader(loadFixture(t))).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	var siaEarlyDelete, ziaEarlyDelete, girEarlyDelete bool

	for _, r := range records {
		if r.PricingDimension == "fee" {
			switch r.SKU {
			case "26EY5Q656QEFV8J2":
				siaEarlyDelete = true
			case "YQHTRZZKTT737SRB":
				ziaEarlyDelete = true
			case "GCDNDUMKKKSY8SH8":
				girEarlyDelete = true
			}
		}
	}

	if !siaEarlyDelete {
		t.Error("SIA Early Delete fee was not preserved")
	}
	if !ziaEarlyDelete {
		t.Error("ZIA Early Delete fee was not preserved")
	}
	if !girEarlyDelete {
		t.Error("GIR Early Delete fee was not preserved")
	}
}

func TestManagementFeatures(t *testing.T) {
	records, err := New(bytes.NewReader(loadFixture(t))).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	var storageLensTiers int
	var inventory, objectTagging, batchJobs, batchObjects bool

	for _, r := range records {
		if r.PricingDimension == "management" {
			switch r.SKU {
			case "PWNMYRK4GT93REV7":
				storageLensTiers++
			case "GV4UH3C2SAN3EC98":
				inventory = true
			case "G76SDFUV63NSS6N2":
				objectTagging = true
			case "JS698V37SA2BFFYW":
				batchJobs = true
			case "VFSW6ADYJ5NS2Z6P":
				batchObjects = true
			}
		}
	}

	if storageLensTiers != 3 {
		t.Errorf("expected 3 tiered records for Storage Lens, got %d", storageLensTiers)
	}
	if !inventory {
		t.Error("S3 Inventory pricing was not preserved")
	}
	if !objectTagging {
		t.Error("S3 Object Tagging pricing was not preserved")
	}
	if !batchJobs {
		t.Error("S3 Batch Operations Jobs pricing was not preserved")
	}
	if !batchObjects {
		t.Error("S3 Batch Operations Objects pricing was not preserved")
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := New(bytes.NewReader(loadFixture(t)))
	_, err := c.Fetch(ctx)
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
}

func TestInvalidJSON(t *testing.T) {
	c := New(strings.NewReader("invalid json"))
	_, err := c.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
}

func TestEmptyProducts(t *testing.T) {
	c := New(strings.NewReader(`{"formatVersion": "v1.0", "products": {}}`))
	_, err := c.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error on empty products, got nil")
	}
}
