package dynamodb

import (
	"os"
	"testing"
)

func TestParseOffer(t *testing.T) {
	data, err := os.ReadFile("testdata/offer.json")
	if err != nil {
		t.Fatal(err)
	}

	records, err := parseOffer(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 38 {
		t.Fatalf("got %d pricing records, want 38", len(records))
	}
}
