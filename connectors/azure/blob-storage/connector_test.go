package blobstorage

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"pricing-catalog/pkg/connector"
	"pricing-catalog/pkg/connector/testkit"
	"testing"
)

func TestConnectorContract(t *testing.T) {
	newConnector := func() connector.Connector {
		data, err := os.ReadFile(filepath.Join("testdata", "source.json"))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}

		c := New()

		c.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(data)),
			}, nil
		})

		return c
	}

	records := testkit.AssertConnectorContract(t, newConnector)

	for _, record := range records {
		if record.Provider != "azure" {
			t.Fatalf("invalid provider: %s", record.Provider)
		}

		if record.Product != "Blob Storage" {
			t.Fatalf("invalid product: %s", record.Product)
		}

		if record.ProviderProductID != "DZH318Z0BNZJ" {
			t.Fatalf("invalid product ID: %s", record.ProviderProductID)
		}

		if record.Source.Type != "azure_retail_prices" {
			t.Fatalf("invalid source type: %s", record.Source.Type)
		}

		if record.Source.URL != URL {
			t.Fatalf("invalid URL: %s", record.Source.URL)
		}

		if record.Attributes["service_name"] != "Storage" {
			t.Fatalf("invalid service name: %s", record.Attributes["service_name"])
		}

		if record.Attributes["service_id"] != "DZH317F1HKN0" {
			t.Fatalf("invalid service id: %s", record.Attributes["service_id"])
		}

		if record.Attributes["service_family"] != "Storage" {
			t.Fatalf("invalid service family: %s", record.Attributes["service_family"])
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
