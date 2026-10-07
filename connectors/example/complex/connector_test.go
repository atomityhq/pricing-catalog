package examplecomplex

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"pricing-catalog/pkg/connector"
	"pricing-catalog/pkg/connector/testkit"
)

func TestConnectorContract(t *testing.T) {
	newConnector := func() connector.Connector {
		data, err := os.ReadFile(filepath.Join("testdata", "source.json"))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}

		return New(bytes.NewReader(data))
	}

	records := testkit.AssertConnectorContract(t, newConnector)

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
