package aws_ebs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"pricing-catalog/pkg/catalog"
)

const (
	name      = "aws_ebs"
	SourceURL = "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonEC2/current/us-east-1/index.json"
	TimeOUT   = 30 * time.Second
	maxSize   = 256 << 20 // Maximum input size: 256 MiB.
)

type Connector struct {
	httpClient *http.Client
	sourceURL  string
	reader     io.Reader
}

func New(reader io.Reader) *Connector {
	return &Connector{
		httpClient: &http.Client{Timeout: TimeOUT},
		sourceURL:  SourceURL,
		reader:     reader,
	}
}

func (c *Connector) Name() string { return name }

func (c *Connector) FetchData(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	reader := c.reader
	if reader == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.sourceURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create AWS request: %w", err)
		}
		req.Header.Set("Accept", "application/json")

		res, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch AWS offer: %w", err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to fetch data: %s", res.Status)
		}
		if res.ContentLength > maxSize {
			return nil, fmt.Errorf("response body too large: %d bytes", res.ContentLength)
		}
		reader = res.Body
	}

	data, err := readBody(reader, maxSize)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Connector) Fetch(ctx context.Context) ([]catalog.PricingRecord, error) {
	data, err := c.FetchData(ctx)
	if err != nil {
		return nil, err
	}
	return normalizeData(data)
}

func readBody(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read AWS offer: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response body too large: limit is %d bytes", limit)
	}
	return data, nil
}
