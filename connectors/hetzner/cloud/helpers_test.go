package cloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// readFixture loads testdata/price.json. Both connector and normalize tests use
// this helper; run the whole package (go test ./connectors/hetzner/cloud/), not
// a single *_test.go file in isolation.
func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "price.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func editFixture(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(readFixture(t), &root); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	mutate(root)
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatalf("marshal edited fixture: %v", err)
	}
	return data
}
