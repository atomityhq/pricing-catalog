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
}
