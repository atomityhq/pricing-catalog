package azure_disk

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
	"pricing-catalog/pkg/connector/testkit"
)

func TestConnectorContract(t *testing.T) {
	fixtureData, err := os.ReadFile(filepath.Join("testdata", "pricing.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	factory := func() connector.Connector {
		return New(bytes.NewReader(fixtureData))
	}

	records := testkit.AssertConnectorContract(t, factory)
	connectorRecords, err := New(bytes.NewReader(fixtureData)).Fetch(context.Background())
	if err != nil {
		t.Fatalf("direct connector fetch: %v", err)
	}
	records = connectorRecords

	countsByProductModel := make(map[string]int)
	reservationKeysByTerm := make(map[string]string)
	for _, r := range records {
		if r.Provider != "azure" {
			t.Errorf("expected provider 'azure', got %q", r.Provider)
		}
		if r.Product == "Blob Storage" {
			t.Errorf("non-disk product was not filtered out: %s", r.Product)
		}
		if r.PurchaseModel != "on_demand" && r.PurchaseModel != "reserved" {
			t.Errorf("unexpected purchase_model %q", r.PurchaseModel)
		}
		countsByProductModel[r.Product+"/"+r.PurchaseModel]++
		if r.Region != "eastus" && r.Region != "westus2" {
			t.Errorf("unexpected region %q", r.Region)
		}
		if r.BillingUnit == "" {
			t.Error("billing_unit must not be empty")
		}
		if r.Price.Currency != "USD" {
			t.Errorf("expected currency 'USD', got %q", r.Price.Currency)
		}
		for key, value := range r.Dimensions {
			if value == "" {
				t.Errorf("dimension %q must not be empty", key)
			}
		}
		if r.SKU == "P80 LRS" && r.PurchaseModel == "reserved" {
			term := r.Dimensions["reservationTerm"]
			if term != "1 Year" && term != "10 Years" {
				t.Errorf("unexpected P80 reservation term %q", term)
			}
			reservationKeysByTerm[term] = catalog.RecordKey(r)
		}
		if r.SKU == "P80 LRS" {
			if r.PurchaseModel == "on_demand" {
				if _, ok := r.Dimensions["reservationTerm"]; ok {
					t.Error("consumption record must not contain reservationTerm")
				}
			}
		}
	}
	wantCounts := map[string]int{
		"Azure Premium SSD v2/on_demand":        4,
		"Premium SSD Managed Disks/on_demand":   1,
		"Premium SSD Managed Disks/reserved":    1,
		"Premium SSD Managed Disks_v8/reserved": 1,
		"Standard HDD Managed Disks/on_demand":  4,
		"Standard SSD Managed Disks/on_demand":  4,
		"Ultra Disks/on_demand":                 1,
	}
	if !reflect.DeepEqual(countsByProductModel, wantCounts) {
		t.Errorf("counts by product and purchase model = %#v, want %#v", countsByProductModel, wantCounts)
	}
	if reservationKeysByTerm["1 Year"] == "" || reservationKeysByTerm["10 Years"] == "" {
		t.Errorf("expected P80 LRS reservations for 1 Year and 10 Years; got %#v", reservationKeysByTerm)
	} else if reservationKeysByTerm["1 Year"] == reservationKeysByTerm["10 Years"] {
		t.Error("P80 LRS 1-year and 10-year reservations must have distinct record keys")
	}
	for i := 1; i < len(records); i++ {
		if catalog.RecordKey(records[i-1]) > catalog.RecordKey(records[i]) {
			t.Errorf("records are not sorted by catalog.RecordKey at indexes %d and %d", i-1, i)
			break
		}
	}
}
