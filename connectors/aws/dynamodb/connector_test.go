package dynamodb

import (
	"bytes"
	"os"
	"testing"

	"pricing-catalog/pkg/connector"
	"pricing-catalog/pkg/connector/testkit"
)

func TestConnectorContract(t *testing.T) {
	data, err := os.ReadFile("testdata/offer.json")
	if err != nil {
		t.Fatal(err)
	}

	records := testkit.AssertConnectorContract(t, func() connector.Connector {
		return New(bytes.NewReader(data))
	})

	if len(records) != 46 {
		t.Fatalf("got %d pricing records, want 46", len(records))
	}

	checks := []struct {
		name          string
		sku           string
		purchaseModel string
		unit          string
		lease         string
		amount        string
		tierStart     string
		tierEnd       string
	}{
		{"read request", "4W4ZMC46EHE8XTTZ", "on_demand", "ReadRequestUnits", "", "0.0000001250", "0", ""},
		{"free write capacity", "R6PXMNYCEDGZ2EYN", "on_demand", "WriteCapacityUnit-Hrs", "", "0.0000000000", "0", "18600"},
		{"paid write capacity", "R6PXMNYCEDGZ2EYN", "on_demand", "WriteCapacityUnit-Hrs", "", "0.0006500000", "18600", ""},
		{"one-year upfront fee", "R6PXMNYCEDGZ2EYN", "reserved", "Quantity", "1yr", "1.5", "", ""},
		{"three-year upfront fee", "R6PXMNYCEDGZ2EYN", "reserved", "Quantity", "3yr", "1.8", "", ""},
	}

	for _, want := range checks {
		t.Run(want.name, func(t *testing.T) {
			for _, got := range records {
				if got.SKU != want.sku ||
					got.Region != "us-east-1" ||
					got.PurchaseModel != want.purchaseModel ||
					got.BillingUnit != want.unit ||
					got.Dimensions["lease_contract_length"] != want.lease ||
					got.Price.Currency != "USD" ||
					got.Price.Amount != want.amount {
					continue
				}

				if want.tierStart == "" && got.Tier == nil {
					return
				}
				if got.Tier != nil &&
					got.Tier.StartQuantity == want.tierStart &&
					got.Tier.EndQuantity == want.tierEnd {
					return
				}
			}
			t.Errorf("missing expected pricing record: %+v", want)
		})
	}
}
