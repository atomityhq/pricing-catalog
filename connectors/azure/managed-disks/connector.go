// Package azuremanageddisks is the pricing-catalog connector for Azure
// Managed Disks. It reads the public Azure Retail Prices API
// (https://prices.azure.com/api/retail/prices). See DESIGN.md.
package azuremanageddisks

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
)

const (
	providerName  = "azure"
	connectorName = "azure-managed-disks"

	// SourceURL is the Azure Retail Prices API endpoint.
	SourceURL  = "https://prices.azure.com/api/retail/prices"
	sourceType = "azure_retail_prices_api"

	// sourceFilter narrows the API to the Managed Disks price lists. The
	// connector applies the same product scope again while normalizing, so the
	// filter is an optimization, not the source of truth for scope.
	sourceFilter = "serviceName eq 'Storage' and (contains(productName, 'Managed Disks') " +
		"or productName eq 'Azure Premium SSD v2' or productName eq 'Ultra Disks')"

	requestTimeout = 30 * time.Second
	maxAttempts    = 3
)

var _ connector.Connector = (*Connector)(nil)

// Connector fetches Azure Managed Disks prices and normalizes them into
// canonical pricing records.
type Connector struct {
	reader     io.Reader
	endpoint   string
	httpClient *http.Client
	retryWait  time.Duration
}

// New returns a connector that reads a saved Azure Retail Prices API page
// from reader instead of calling the API. Use a fresh reader per connector.
func New(reader io.Reader) *Connector {
	c := NewLive()
	c.reader = reader
	return c
}

// NewLive returns a connector that pages through the live Retail Prices API.
func NewLive() *Connector {
	return &Connector{
		endpoint:   SourceURL,
		httpClient: &http.Client{Timeout: requestTimeout},
		retryWait:  2 * time.Second,
	}
}

// Name returns the connector identifier.
func (c *Connector) Name() string { return connectorName }

// Fetch loads the source and returns canonical pricing records.
func (c *Connector) Fetch(ctx context.Context) ([]catalog.PricingRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var (
		items []retailPriceItem
		err   error
	)
	if c.reader != nil {
		var page *retailPricePage
		page, err = decodePage(c.reader)
		if err == nil {
			items = *page.Items
		}
	} else {
		items, err = c.fetchAll(ctx)
	}
	if err != nil {
		return nil, &connector.SourceError{Connector: connectorName, Err: err}
	}

	records, err := normalize(items, sourceType, SourceURL)
	if err != nil {
		return nil, fmt.Errorf("normalize Azure managed disk prices: %w", err)
	}
	return records, nil
}

// fetchAll follows NextPageLink until the last page.
func (c *Connector) fetchAll(ctx context.Context) ([]retailPriceItem, error) {
	u, err := url.Parse(c.endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	q := u.Query()
	q.Set("$filter", sourceFilter)
	u.RawQuery = q.Encode()

	var items []retailPriceItem
	for next := u.String(); next != ""; {
		page, err := c.fetchPage(ctx, next)
		if err != nil {
			return nil, err
		}
		items = append(items, *page.Items...)
		next = page.NextPageLink
	}
	return items, nil
}

// fetchPage requests one page. Throttling (HTTP 429) and server errors (5xx)
// are temporary, so they are tried up to maxAttempts times, waiting a little
// longer each time. Any other error stops the fetch.
func (c *Connector) fetchPage(ctx context.Context, pageURL string) (*retailPricePage, error) {
	for attempt := 1; ; attempt++ {
		page, retry, err := c.tryPage(ctx, pageURL)
		if err == nil {
			return page, nil
		}
		if !retry || attempt == maxAttempts {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * c.retryWait):
		}
	}
}

// tryPage sends one request. retry reports whether the error is temporary.
func (c *Connector) tryPage(ctx context.Context, pageURL string) (page *retailPricePage, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, true, fmt.Errorf("request %s: %w", pageURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("request %s: HTTP %d", pageURL, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("request %s: HTTP %d", pageURL, resp.StatusCode)
	}

	page, err = decodePage(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("page %s: %w", pageURL, err)
	}
	return page, false, nil
}
