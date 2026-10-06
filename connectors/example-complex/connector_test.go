package examplecomplex

import (
	"os"
	"path/filepath"
	"testing"

	"pricing-catalog/pkg/connector/testkit"
)

func TestConnectorContract(t *testing.T) {
	path := filepath.Join("testdata", "source.json")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	records := testkit.AssertConnectorContract(t, New(file))
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	var onDemand, committed int
	var tiered int
	for _, record := range records {
		switch record.PurchaseModel {
		case "on_demand":
			onDemand++
		case "committed":
			committed++
		}
		if record.Tier != nil {
			tiered++
		}
	}
	if onDemand != 2 || committed != 1 {
		t.Fatalf("purchase model information was lost: on_demand=%d committed=%d", onDemand, committed)
	}
	if tiered != 3 {
		t.Fatalf("expected all three records to preserve tier information, got %d", tiered)
	}

	if records[0].Dimensions["vcpus"] != "4" || records[0].Dimensions["memory_gib"] != "16" {
		t.Fatalf("resource dimensions were not preserved: %#v", records[0].Dimensions)
	}
}
