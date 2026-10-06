package catalog

import (
	"encoding/json"
	"fmt"
	"io"
)

// LoadJSON loads and validates a catalog snapshot from JSON.
func LoadJSON(r io.Reader) (Snapshot, error) {
	var snapshot Snapshot
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Snapshot{}, fmt.Errorf("snapshot contains multiple JSON values")
		}
		return Snapshot{}, fmt.Errorf("decode trailing snapshot data: %w", err)
	}

	if snapshot.Version == "" {
		return Snapshot{}, fmt.Errorf("snapshot.version is required")
	}
	if snapshot.GeneratedAt.IsZero() {
		return Snapshot{}, fmt.Errorf("snapshot.generated_at is required")
	}

	if err := ValidateRecords(snapshot.Records); err != nil {
		return Snapshot{}, fmt.Errorf("validate snapshot: %w", err)
	}

	return snapshot, nil
}
