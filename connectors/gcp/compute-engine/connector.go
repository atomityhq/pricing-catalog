// Package gcpcomputeengine implements the pricing-catalog connector for
// Google Cloud Compute Engine (services/6F81-5844-456A).
package gcpcomputeengine

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"time"

	"pricing-catalog/pkg/catalog"
)

const (
	providerName      = "gcp"
	connectorName     = "gcp-compute-engine"
	serviceID         = "services/6F81-5844-456A"
	defaultCatalogURL = "https://cloudbilling.googleapis.com/v1/services/6F81-5844-456A/skus"
	defaultSourceType = "gcp_cloud_billing_catalog"
)

// Option configures a Connector instance.
type Option func(*Connector)

// WithBaseURL overrides the default Cloud Billing Catalog API endpoint URL.
func WithBaseURL(catalogURL string) Option {
	return func(c *Connector) {
		c.catalogURL = catalogURL
	}
}

// WithHTTPClient sets a custom HTTP client for API requests.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Connector) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithAPIKey sets the Google Cloud API key used when querying the public endpoint.
func WithAPIKey(apiKey string) Option {
	return func(c *Connector) {
		c.apiKey = apiKey
	}
}

// WithReader configures the connector to decode SKU data directly from an io.Reader
// instead of performing live HTTP requests.
func WithReader(r io.Reader) Option {
	return func(c *Connector) {
		c.reader = r
	}
}

// Connector fetches and normalizes Google Compute Engine pricing data into
// canonical catalog.PricingRecord entries.
type Connector struct {
	catalogURL string
	serviceID  string
	apiKey     string
	httpClient *http.Client
	reader     io.Reader
}

// New creates a new Google Compute Engine Connector with the provided options.
func New(opts ...Option) *Connector {
	c := &Connector{
		catalogURL: defaultCatalogURL,
		serviceID:  serviceID,
		apiKey:     os.Getenv("GCP_API_KEY"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name returns the unique identifier of the connector.
func (c *Connector) Name() string {
	return connectorName
}

// Fetch retrieves raw Compute Engine SKUs from the configured source, normalizes
// them into canonical pricing records, and returns them in deterministic order.
func (c *Connector) Fetch(ctx context.Context) ([]catalog.PricingRecord, error) {
	skus, err := c.loadSKUs(ctx)
	if err != nil {
		return nil, fmt.Errorf("load SKUs: %w", err)
	}

	records, err := c.normalize(skus)
	if err != nil {
		return nil, fmt.Errorf("normalize SKUs: %w", err)
	}

	sortRecords(records)
	return records, nil
}

// loadSKUs reads SKU definitions either from the injected io.Reader fixture
// or by paging through the HTTP catalog endpoint.
func (c *Connector) loadSKUs(ctx context.Context) ([]gcpSKU, error) {
	if c.reader != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var resp gcpResponse
		if err := json.NewDecoder(c.reader).Decode(&resp); err != nil {
			return nil, fmt.Errorf("decode reader source: %w", err)
		}
		return resp.Skus, nil
	}

	return c.fetchAllSKUs(ctx)
}

// fetchAllSKUs iterates through all pages of the Cloud Billing Catalog API response
// using nextPageToken until all SKUs are collected.
func (c *Connector) fetchAllSKUs(ctx context.Context) ([]gcpSKU, error) {
	var allSKUs []gcpSKU
	pageToken := ""

	for {
		page, err := c.fetchPage(ctx, pageToken)
		if err != nil {
			return nil, err
		}

		allSKUs = append(allSKUs, page.Skus...)

		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}

	return allSKUs, nil
}

// fetchPage requests a single page of SKUs from the configured HTTP endpoint.
func (c *Connector) fetchPage(ctx context.Context, pageToken string) (*gcpResponse, error) {
	u, err := url.Parse(c.catalogURL)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	q := u.Query()
	if c.apiKey != "" {
		q.Set("key", c.apiKey)
	}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from GCP API", resp.StatusCode)
	}

	var result gcpResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// sortRecords sorts canonical pricing records deterministically by their canonical key.
func sortRecords(records []catalog.PricingRecord) {
	slices.SortFunc(records, func(a, b catalog.PricingRecord) int {
		return cmp.Compare(catalog.RecordKey(a), catalog.RecordKey(b))
	})
}
