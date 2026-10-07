package examplecomplex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"pricing-catalog/pkg/catalog"
)

type source struct {
	Provider string `json:"provider"`
	Prices   []struct {
		ProductID string            `json:"product_id"`
		Product   string            `json:"product"`
		SKU       string            `json:"sku"`
		Region    string            `json:"region"`
		Model     string            `json:"purchase_model"`
		Unit      string            `json:"unit"`
		Currency  string            `json:"currency"`
		Dimension map[string]string `json:"dimensions"`
		Tiers     []struct {
			From  string `json:"from"`
			To    string `json:"to"`
			Price string `json:"price"`
		} `json:"tiers"`
	} `json:"prices"`
}

type Connector struct {
	reader io.Reader
}

func New(reader io.Reader) Connector {
	return Connector{reader: reader}
}

func (c Connector) Name() string { return "example-complex" }

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

	records := make([]catalog.PricingRecord, 0)

	for _, item := range raw.Prices {
		for _, tier := range item.Tiers {
			dimensions := make(map[string]string, len(item.Dimension))
			for key, value := range item.Dimension {
				dimensions[key] = value
			}

			records = append(records, catalog.PricingRecord{
				Provider:          raw.Provider,
				Product:           item.Product,
				SKU:               item.SKU,
				ProviderProductID: item.ProductID,
				ProviderSKUID:     item.SKU,
				Region:            item.Region,
				PurchaseModel:     item.Model,
				BillingUnit:       item.Unit,
				PricingDimension:  "resource",
				Dimensions:        dimensions,
				Price:             catalog.Money{Amount: tier.Price, Currency: item.Currency},
				Tier: &catalog.Tier{
					StartQuantity: tier.From,
					EndQuantity:   tier.To,
				},
				Source: catalog.Source{
					Type: "fixture",
					URL:  "https://example.invalid/pricing",
				},
			})
		}
	}

	return records, nil
}
