package contract_test

import (
	"testing"

	"pricing-catalog/pkg/catalog"
)

func TestCanonicalRecordContract(t *testing.T) {
	record := catalog.PricingRecord{
		Provider:         "example-cloud",
		Product:          "VM",
		SKU:              "example-vm-1",
		Region:           "eu-west",
		PurchaseModel:    "on_demand",
		BillingUnit:      "hour",
		PricingDimension: "unit",
		Price:            catalog.Money{Amount: "0.01", Currency: "EUR"},
		Source: catalog.Source{
			Type: "fixture",
			URL:  "https://example.invalid/pricing",
		},
	}

	if err := catalog.ValidateRecord(record); err != nil {
		t.Fatalf("canonical record should validate: %v", err)
	}
}
