package cloud

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
)

const (
	connectorName  = "hetzner-cloud"
	defaultURL     = "https://api.hetzner.cloud/v1/pricing"
	defaultTimeout = 30 * time.Second
	maxBodyBytes   = 32 << 20
)

// Option configures a Connector.
type Option func(*Connector)

// WithHTTPClient sets the HTTP client used for live fetches.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Connector) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithSourceURL overrides the Hetzner pricing endpoint.
func WithSourceURL(url string) Option {
	return func(c *Connector) {
		if strings.TrimSpace(url) != "" {
			c.sourceURL = url
		}
	}
}

// WithReader configures the connector to decode a captured pricing payload
// instead of performing a live HTTP request.
func WithReader(r io.Reader) Option {
	return func(c *Connector) {
		c.reader = r
	}
}

// WithToken sets the Hetzner Cloud API token used for live requests.
func WithToken(token string) Option {
	return func(c *Connector) {
		c.token = token
	}
}

// Connector fetches and normalizes Hetzner Cloud pricing into canonical records.
type Connector struct {
	httpClient *http.Client
	sourceURL  string
	reader     io.Reader
	token      string
}

// New creates a Hetzner Cloud connector. Tests should pass WithReader so they
// never depend on the live API or credentials.
func New(opts ...Option) *Connector {
	c := &Connector{
		httpClient: &http.Client{Timeout: defaultTimeout},
		sourceURL:  defaultURL,
		token:      os.Getenv("HCLOUD_TOKEN"),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name returns the stable connector identifier.
func (c *Connector) Name() string { return connectorName }

// Fetch loads provider pricing data and returns canonical records.
func (c *Connector) Fetch(ctx context.Context) ([]catalog.PricingRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, &connector.SourceError{Connector: connectorName, Err: err}
	}

	data, sourceURL, err := c.load(ctx)
	if err != nil {
		return nil, &connector.SourceError{Connector: connectorName, Err: err}
	}

	records, err := normalizePricing(data, sourceURL)
	if err != nil {
		return nil, &connector.SourceError{Connector: connectorName, Err: err}
	}
	return records, nil
}

func (c *Connector) load(ctx context.Context) ([]byte, string, error) {
	sourceURL := c.sourceURL
	if strings.TrimSpace(sourceURL) == "" {
		sourceURL = defaultURL
	}

	if c.reader != nil {
		data, err := readBody(c.reader, maxBodyBytes)
		if err != nil {
			return nil, "", err
		}
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		return data, sourceURL, nil
	}

	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(c.token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch pricing from %s: %w", sourceURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch pricing from %s: unexpected status %d", sourceURL, resp.StatusCode)
	}

	data, err := readBody(resp.Body, maxBodyBytes)
	if err != nil {
		return nil, "", err
	}
	return data, sourceURL, nil
}

func readBody(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read pricing payload: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("pricing payload exceeds %d bytes", limit)
	}
	return data, nil
}
