package azuremanageddisks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pricing-catalog/internal/pipeline"
	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
	"pricing-catalog/pkg/connector/testkit"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func fixtureRecords(t *testing.T, name string) []catalog.PricingRecord {
	t.Helper()
	data := readFixture(t, name)
	return testkit.AssertConnectorContract(t, func() connector.Connector {
		return New(bytes.NewReader(data))
	})
}

func buildSnapshot(data []byte) (catalog.Snapshot, error) {
	return pipeline.BuildSnapshot(context.Background(), New(bytes.NewReader(data)),
		pipeline.BuildOptions{Version: "test", GeneratedAt: time.Unix(0, 0).UTC()})
}

func TestConnectorContract(t *testing.T) {
	records := fixtureRecords(t, "retail_prices.json")

	// 39 source items: one is an out-of-scope Premium Page Blob (unmanaged
	// disk) price; every other item becomes exactly one record.
	if len(records) != 38 {
		t.Fatalf("got %d records, want 38", len(records))
	}
	for _, r := range records {
		if r.Product == "Premium Page Blob" {
			t.Fatalf("out-of-scope product was normalized: %#v", r)
		}
		if r.Provider != "azure" || r.Source.Type != sourceType || r.Source.URL != SourceURL {
			t.Fatalf("unexpected provider/source: %#v", r)
		}
	}
}

func TestConnectorContractEUR(t *testing.T) {
	records := fixtureRecords(t, "retail_prices_eur.json")
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3", len(records))
	}

	disk := findRecord(t, records, "DZH318Z0BP04/00DJ", "westeurope", "disk", "on_demand", "")
	// Trailing zero kept exactly as published.
	if disk.Price != (catalog.Money{Amount: "133.4710", Currency: "EUR"}) {
		t.Fatalf("EUR price = %#v", disk.Price)
	}
}

func TestName(t *testing.T) {
	if got := New(nil).Name(); got != "azure-managed-disks" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestDeterministicRegardlessOfInputOrder(t *testing.T) {
	var page struct {
		Items []json.RawMessage `json:"Items"`
	}
	if err := json.Unmarshal(readFixture(t, "retail_prices.json"), &page); err != nil {
		t.Fatal(err)
	}
	reversed := make([]json.RawMessage, len(page.Items))
	for i, item := range page.Items {
		reversed[len(page.Items)-1-i] = item
	}
	reversedJSON, err := json.Marshal(map[string]any{"Items": reversed})
	if err != nil {
		t.Fatal(err)
	}

	first, err := buildSnapshot(readFixture(t, "retail_prices.json"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildSnapshot(reversedJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("snapshot depends on source item order")
	}
}

func TestFetchReaderErrors(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"empty", "", "decode Azure retail prices JSON"},
		{"malformed", `{"Items": [`, "decode Azure retail prices JSON"},
		{"not a page", `[{"productName": "Ultra Disks"}]`, "decode Azure retail prices JSON"},
		{"no items array", `{"BillingCurrency": "USD"}`, `no "Items" array`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(strings.NewReader(tc.body)).Fetch(context.Background())
			var sourceErr *connector.SourceError
			if !errors.As(err, &sourceErr) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want SourceError containing %q", err, tc.want)
			}
		})
	}
}

func TestOnlyOutOfScopeItemsFailInPipeline(t *testing.T) {
	_, err := buildSnapshot([]byte(`{"Items": [{"productName": "Blob Storage", "skuId": "x"}]}`))
	if !errors.Is(err, pipeline.ErrEmptyCatalog) {
		t.Fatalf("err = %v, want ErrEmptyCatalog", err)
	}
}

func TestFetchCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(bytes.NewReader(readFixture(t, "retail_prices.json"))).Fetch(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// splitFixture serves the fixture as two pages linked by NextPageLink.
func splitFixture(t *testing.T) (first, second []json.RawMessage) {
	t.Helper()
	var page struct {
		Items []json.RawMessage `json:"Items"`
	}
	if err := json.Unmarshal(readFixture(t, "retail_prices.json"), &page); err != nil {
		t.Fatal(err)
	}
	half := len(page.Items) / 2
	return page.Items[:half], page.Items[half:]
}

func writePage(t *testing.T, w http.ResponseWriter, items []json.RawMessage, next string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{"BillingCurrency": "USD", "Items": items, "NextPageLink": nil, "Count": len(items)}
	if next != "" {
		body["NextPageLink"] = next
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Error(err)
	}
}

// liveTestConnector points a live connector at a local test server and
// removes the waiting between retries.
func liveTestConnector(server *httptest.Server) *Connector {
	c := NewLive()
	c.endpoint = server.URL
	c.httpClient = server.Client()
	c.retryWait = 0
	return c
}

func TestFetchLivePagination(t *testing.T) {
	first, second := splitFixture(t)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept header = %q", r.Header.Get("Accept"))
		}
		switch r.URL.Query().Get("page") {
		case "":
			if got := r.URL.Query().Get("$filter"); got != sourceFilter {
				t.Errorf("$filter = %q", got)
			}
			writePage(t, w, first, server.URL+"/?page=2")
		case "2":
			writePage(t, w, second, "")
		default:
			t.Errorf("unexpected page %q", r.URL.RawQuery)
		}
	}))
	defer server.Close()

	records, err := liveTestConnector(server).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 38 {
		t.Fatalf("got %d records across pages, want 38", len(records))
	}
}

func TestFetchLiveRetriesThrottling(t *testing.T) {
	first, _ := splitFixture(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			writePage(t, w, first, "")
		}
	}))
	defer server.Close()

	if _, err := liveTestConnector(server).Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestFetchLiveErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantCalls int32
		want      string
	}{
		{"not found is not retried", http.StatusNotFound, `{}`, 1, "HTTP 404"},
		{"server error gives up after maxAttempts", http.StatusInternalServerError, `{}`, maxAttempts, "HTTP 500"},
		{"malformed JSON", http.StatusOK, `{"Items": [`, 1, "decode Azure retail prices JSON"},
		{"missing Items", http.StatusOK, `{"error": "something went wrong"}`, 1, `no "Items" array`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			_, err := liveTestConnector(server).Fetch(context.Background())
			var sourceErr *connector.SourceError
			if !errors.As(err, &sourceErr) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want SourceError containing %q", err, tc.want)
			}
			if calls.Load() != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", calls.Load(), tc.wantCalls)
			}
		})
	}
}

func TestFetchLiveCancelledWhileWaiting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c := liveTestConnector(server)
	c.retryWait = time.Hour
	_, err := c.Fetch(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}
