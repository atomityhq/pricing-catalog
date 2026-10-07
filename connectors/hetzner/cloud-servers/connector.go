package cloudservers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"pricing-catalog/pkg/catalog"
)

type source struct {
	Pricing struct {
		Currency    string `json:"currency"`
		ServerTypes []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Prices []struct {
				Location string `json:"location"`
				Hourly   struct {
					Net string `json:"net"`
				} `json:"price_hourly"`
				Monthly struct {
					Net string `json:"net"`
				} `json:"price_monthly"`
			} `json:"prices"`
		} `json:"server_types"`
	} `json:"pricing"`
}

type Connector struct {
	reader io.Reader
}

func New(reader io.Reader) Connector {
	return Connector{reader: reader}
}

func (c Connector) Name() string {
	return "hetzner-cloud-servers"
}

func (c Connector) Fetch(ctx context.Context) ([]catalog.PricingRecord, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var raw source
	if err := json.NewDecoder(c.reader).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode Hetzner pricing: %w", err)
	}

	records := make([]catalog.PricingRecord, 0)

	for _, serverType := range raw.Pricing.ServerTypes {
		for _, price := range serverType.Prices {
			records = append(records,
				newRecord(
					raw.Pricing.Currency,
					serverType.ID,
					serverType.Name,
					price.Location,
					"hour",
					price.Hourly.Net,
				),
				newRecord(
					raw.Pricing.Currency,
					serverType.ID,
					serverType.Name,
					price.Location,
					"month",
					price.Monthly.Net,
				),
			)
		}
	}

	return records, nil
}

func newRecord(
	currency string,
	serverTypeID int64,
	serverType string,
	location string,
	unit string,
	amount string,
) catalog.PricingRecord {
	return catalog.PricingRecord{
		Provider:          "hetzner",
		Product:           "cloud-server",
		SKU:               serverType,
		ProviderProductID: "server_type",
		ProviderSKUID:     fmt.Sprintf("%d", serverTypeID),
		Region:            location,
		PurchaseModel:     "on_demand",
		BillingUnit:       unit,
		PricingDimension:  "server",
		Price: catalog.Money{
			Amount:   amount,
			Currency: currency,
		},
		Source: catalog.Source{
			Type: "hetzner-cloud-pricing-api",
			URL:  "https://api.hetzner.cloud/v1/pricing",
		},
	}
}
