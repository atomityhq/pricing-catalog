package pipeline

import (
	"encoding/json"
	"fmt"
	"io"

	"pricing-catalog/pkg/catalog"
)

// WriteSnapshot writes a validated snapshot using stable formatting.
//
// Snapshot generation belongs to the pipeline rather than provider
// connectors. Connectors should return canonical PricingRecord values and
// should not write catalog files themselves.
func WriteSnapshot(w io.Writer, snapshot catalog.Snapshot) error {
	normalized, err := catalog.FromSnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("validate snapshot before write: %w", err)
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(normalized.Snapshot()); err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}

	return nil
}
