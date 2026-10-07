package examplesimple

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"pricing-catalog/pkg/catalog"
)

type source struct {
	Provider string `json:"provider"`
	Products []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		SKU      string `json:"sku"`
		Region   string `json:"region"`
		Unit     string `json:"unit"`
		Currency string `json:"currency"`
		Price    string `json:"price"`
	} `json:"products"`
}

// Connector is a fixture-backed reference connector demonstrating the basic
// provider -> canonical mapping flow. A real connector would replace the
// reader with an HTTP/API client or provider catalog reader.
type Connector struct {
	reader io.Reader
}

func New(reader io.Reader) Connector {
	return Connector{reader: reader}
}

func (c Connector) Name() string { return "example-simple" }

func (c Connector) Fetch(ctx context.Context) ([]catalog.PricingRecord, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var raw source
	if err := json.NewDecoder(c.reader).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode source: %w", err)
	}

	records := make([]catalog.PricingRecord, 0, len(raw.Products))
	for _, item := range raw.Products {
		records = append(records, catalog.PricingRecord{
			Provider:          raw.Provider,
			Product:           item.Name,
			SKU:               item.SKU,
			ProviderProductID: item.ID,
			ProviderSKUID:     item.SKU,
			Region:            item.Region,
			PurchaseModel:     "on_demand",
			BillingUnit:       item.Unit,
			PricingDimension:  "unit",
			Price:             catalog.Money{Amount: item.Price, Currency: item.Currency},
			Source: catalog.Source{
				Type: "fixture",
				URL:  "https://example.invalid/pricing",
			},
		})
	}

	return records, nil
}
