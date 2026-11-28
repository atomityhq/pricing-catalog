package functions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"pricing-catalog/pkg/catalog"
)

const (
	Name = "azure_functions"

	SourceURL = "https://prices.azure.com/api/retail/prices" +
		"?api-version=2023-01-01-preview" +
		"&$filter=serviceName%20eq%20%27Functions%27"

	SourceType = "azure_retail_prices_api"

	Timeout = 30 * time.Second
)

type Connector struct {
	httpClient *http.Client
	sourceURL  string
	sourceType string
}

func New() *Connector {
	return &Connector{
		httpClient: &http.Client{
			Timeout: Timeout,
		},
		sourceURL:  SourceURL,
		sourceType: SourceType,
	}
}

func (c *Connector) Name() string {
	return Name
}

func (c *Connector) Fetch(ctx context.Context) ([]catalog.PricingRecord, error) {
	var data AzureResponse

	for nextURL := c.sourceURL; nextURL != ""; {
		request, err := http.NewRequestWithContext(
			ctx, http.MethodGet, nextURL, nil,
		)
		if err != nil {
			return nil, err
		}

		request.Header.Set("Accept", "application/json")

		response, err := c.httpClient.Do(request)
		if err != nil {
			return nil, err
		}

		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf(
				"unexpected status code: %d", response.StatusCode,
			)
		}

		var page AzureResponse
		err = json.NewDecoder(response.Body).Decode(&page)
		response.Body.Close()
		if err != nil {
			return nil, err
		}

		data.Items = append(data.Items, page.Items...)
		nextURL = page.NextPageLink
	}

	if len(data.Items) == 0 {
		return nil, fmt.Errorf("empty Azure pricing response")
	}
	return Normalize(data.Items, c.sourceURL, c.sourceType)
}
