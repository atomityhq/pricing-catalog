package dynamodb

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
)

const sourceURL = "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonDynamoDB/current/us-east-1/index.json"

type Connector struct {
	reader io.Reader
	client *http.Client
}

var _ connector.Connector = (*Connector)(nil)

func New(reader io.Reader) *Connector {
	return &Connector{
		reader: reader,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Connector) Name() string {
	return "aws-dynamodb"
}

func (c *Connector) Fetch(ctx context.Context) ([]catalog.PricingRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	reader := c.reader
	if reader == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create DynamoDB pricing request: %w", err)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch DynamoDB pricing: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch DynamoDB pricing: unexpected HTTP status %s", resp.Status)
		}
		reader = resp.Body
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read DynamoDB pricing: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return parseOffer(data)
}
