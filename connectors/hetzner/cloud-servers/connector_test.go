package cloudservers

import (
	"context"
	"os"
	"testing"

	"pricing-catalog/pkg/catalog"
)

func TestConnector(t *testing.T) {
	file, err := os.Open("testdata/pricing.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	connector := New(file)

	records, err := connector.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 8 {
		t.Fatalf("got %d records, want 8", len(records))
	}

	for _, record := range records {
		if err := catalog.ValidateRecord(record); err != nil {
			t.Fatalf("invalid record: %v", err)
		}
	}
}

func TestConnectorPreservesHourlyAndMonthlyPrices(t *testing.T) {
	file, err := os.Open("testdata/pricing.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	records, err := New(file).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var hourly, monthly bool

	for _, record := range records {
		switch record.BillingUnit {
		case "hour":
			hourly = true
		case "month":
			monthly = true
		}
	}

	if !hourly {
		t.Error("hourly pricing was not preserved")
	}
	if !monthly {
		t.Error("monthly pricing was not preserved")
	}
}
