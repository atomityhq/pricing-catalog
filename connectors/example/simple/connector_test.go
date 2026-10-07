package examplesimple

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
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	byRegion := map[string]string{}
	for _, record := range records {
		byRegion[record.Region] = record.Price.Amount
	}
	if byRegion["eu-west"] != "0.0800" || byRegion["eu-central"] != "0.0850" {
		t.Fatalf("unexpected normalized prices: %#v", byRegion)
	}
}
