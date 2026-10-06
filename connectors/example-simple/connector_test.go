package examplesimple

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
