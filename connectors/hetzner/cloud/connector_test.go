package cloud

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
	"pricing-catalog/pkg/connector/testkit"
)

var _ connector.Connector = (*Connector)(nil)

func TestConnectorContract(t *testing.T) {
	data := readFixture(t)
	records := testkit.AssertConnectorContract(t, func() connector.Connector {
		return New(WithReader(bytes.NewReader(data)))
	})

	if len(records) != 7 {
		t.Fatalf("got %d records, want 7", len(records))
	}

	t.Run("common fields are populated", func(t *testing.T) {
		for _, record := range records {
			if record.Provider != "hetzner" {
				t.Errorf("provider = %q", record.Provider)
			}
			if record.SKU == "" || record.Region == "" || record.PurchaseModel == "" {
				t.Errorf("incomplete identity: %+v", record)
			}
			if record.Price.Currency != "EUR" || record.Price.Amount == "" {
				t.Errorf("price = %+v", record.Price)
			}
			if record.Source.Type != sourceType || record.Source.URL != defaultURL {
				t.Errorf("source = %+v", record.Source)
			}
			if record.BillingUnit == "" || record.PricingDimension == "" {
				t.Errorf("billing fields empty: %+v", record)
			}
		}
	})

	t.Run("covers servers volumes and traffic", func(t *testing.T) {
		products := map[string]int{}
		for _, record := range records {
			products[record.Product]++
		}
		if products["Cloud Server"] == 0 || products["Volume"] != 1 || products["Traffic"] == 0 {
			t.Fatalf("product coverage = %#v", products)
		}
	})
}

func TestNewDefaults(t *testing.T) {
	c := New()
	if c.Name() != connectorName {
		t.Fatalf("Name() = %q", c.Name())
	}
	if c.sourceURL != defaultURL {
		t.Fatalf("sourceURL = %q", c.sourceURL)
	}
	if c.httpClient == nil || c.httpClient.Timeout != defaultTimeout {
		t.Fatalf("http client defaults: %#v", c.httpClient)
	}
}

func TestFetchFromReader(t *testing.T) {
	data := readFixture(t)
	c := New(WithReader(bytes.NewReader(data)), WithSourceURL("https://example.invalid/pricing"))
	records, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(records) != 7 {
		t.Fatalf("got %d records", len(records))
	}
	if records[0].Source.URL != "https://example.invalid/pricing" {
		t.Fatalf("source url = %q", records[0].Source.URL)
	}
}

func TestFetchHTTP(t *testing.T) {
	data := readFixture(t)
	var sawAuth bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		} else {
			sawAuth = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)

	c := New(
		WithHTTPClient(server.Client()),
		WithSourceURL(server.URL),
		WithToken("test-token"),
	)
	records, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !sawAuth {
		t.Fatal("expected Authorization header")
	}
	if len(records) != 7 {
		t.Fatalf("got %d records", len(records))
	}
	if err := catalog.ValidateRecords(records); err != nil {
		t.Fatalf("ValidateRecords: %v", err)
	}
}

func TestFetchHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"not found", `{}`, "unexpected status 404", http.StatusNotFound},
		{"server error", `{}`, "unexpected status 500", http.StatusInternalServerError},
		{"broken JSON", `{"pricing":`, "decode Hetzner pricing JSON", http.StatusOK},
		{"missing currency", `{"pricing":{"server_types":[]}}`, "pricing.currency is required", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)

			c := New(WithHTTPClient(server.Client()), WithSourceURL(server.URL))
			records, err := c.Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) || records != nil {
				t.Fatalf("Fetch = %v, %v; want error containing %q", records, err, tc.want)
			}
		})
	}
}

func TestFetchContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := New(WithReader(bytes.NewReader(readFixture(t))))
	records, err := c.Fetch(ctx)
	if !errors.Is(err, context.Canceled) || records != nil {
		t.Fatalf("Fetch = %v, %v; want context.Canceled", records, err)
	}
}

func TestFetchReaderError(t *testing.T) {
	want := errors.New("reader failed")
	c := New(WithReader(failingReader{err: want}))
	records, err := c.Fetch(context.Background())
	if !errors.Is(err, want) || records != nil {
		t.Fatalf("Fetch = %v, %v", records, err)
	}
}

func TestFetchHonorsTimeoutClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write(readFixture(t))
	}))
	t.Cleanup(server.Close)

	client := server.Client()
	client.Timeout = time.Millisecond
	c := New(WithHTTPClient(client), WithSourceURL(server.URL))
	_, err := c.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }
