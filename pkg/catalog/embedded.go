package catalog

import (
	"bytes"
	_ "embed"
)

//go:embed data/example.json
var exampleSnapshotJSON []byte

// LoadEmbeddedExample demonstrates how a generated catalog snapshot can be
// shipped with the library without requiring runtime access to a provider API.
func LoadEmbeddedExample() (Catalog, error) {
	snapshot, err := LoadJSON(bytes.NewReader(exampleSnapshotJSON))
	if err != nil {
		return Catalog{}, err
	}
	return FromSnapshot(snapshot)
}
