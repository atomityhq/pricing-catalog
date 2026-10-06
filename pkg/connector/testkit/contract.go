package testkit

import (
	"context"
	"testing"
	"time"

	"pricing-catalog/internal/pipeline"
	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
)

// AssertConnectorContract runs the generic checks every connector must pass.
func AssertConnectorContract(t *testing.T, c connector.Connector) []catalog.PricingRecord {
	t.Helper()

	snapshot, err := pipeline.BuildSnapshot(
		context.Background(),
		c,
		pipeline.BuildOptions{
			Version:     "test",
			GeneratedAt: time.Unix(0, 0).UTC(),
		},
	)
	if err != nil {
		t.Fatalf("connector pipeline: %v", err)
	}

	return snapshot.Records
}
