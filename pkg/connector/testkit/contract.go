package testkit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"pricing-catalog/internal/pipeline"
	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
)

// AssertConnectorContract runs the generic checks every connector must pass.
//
// newConnector must return a fresh connector for every invocation. This is
// important for connectors backed by consumable inputs such as io.Reader.
func AssertConnectorContract(
	t *testing.T,
	newConnector func() connector.Connector,
) []catalog.PricingRecord {
	t.Helper()

	options := pipeline.BuildOptions{
		Version:     "test",
		GeneratedAt: time.Unix(0, 0).UTC(),
	}

	first, err := pipeline.BuildSnapshot(
		context.Background(),
		newConnector(),
		options,
	)
	if err != nil {
		t.Fatalf("first connector pipeline: %v", err)
	}

	second, err := pipeline.BuildSnapshot(
		context.Background(),
		newConnector(),
		options,
	)
	if err != nil {
		t.Fatalf("second connector pipeline: %v", err)
	}

	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first snapshot: %v", err)
	}

	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second snapshot: %v", err)
	}

	if string(firstJSON) != string(secondJSON) {
		t.Fatal("connector output is not deterministic")
	}

	return first.Records
}
