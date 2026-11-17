package dynamodb

import (
	"encoding/json"
	"fmt"
	"pricing-catalog/pkg/catalog"
)

type awsOffer struct {
	Products map[string]awsProduct `json:"products"`
	Terms    awsTerms              `json:"terms"`
}

type awsProduct struct {
	Attributes map[string]string `json:"attributes"`
}

type awsTerms struct {
	OnDemand map[string]json.RawMessage `json:"OnDemand"`
}

type awsTerm struct {
	PriceDimension map[string]awsPriceDimension `json:"priceDimensions"`
}

type awsPriceDimension struct {
	Unit         string            `json:"unit"`
	BeginRange   string            `json:"beginRange"`
	EndRange     string            `json:"endRange"`
	PricePerUnit map[string]string `json:"pricePerUnit"`
}

func parseOffer(data []byte) ([]catalog.PricingRecord, error) {
	var offer awsOffer
	if err := json.Unmarshal(data, &offer); err != nil {
		return nil, fmt.Errorf("decode DynamoDB offer: %w", err)
	}

	var records []catalog.PricingRecord
	for sku, termJSON := range offer.Terms.OnDemand {
		product, ok := offer.Products[sku]
		if !ok {
			return nil, fmt.Errorf("SKU %s: missing product", sku)
		}

		var termsForSKU map[string]awsTerm
		if err := json.Unmarshal(termJSON, &termsForSKU); err != nil {
			return nil, fmt.Errorf("SKU %s: decode pricing terms: %w", sku, err)
		}

		for _, term := range termsForSKU {
			for rateCode, position := range term.PriceDimension {
				var tier *catalog.Tier
				if position.BeginRange != "" {
					tier = &catalog.Tier{StartQuantity: position.BeginRange}
					if position.EndRange != "Inf" {
						tier.EndQuantity = position.EndRange
					}
				}

				for currency, amount := range position.PricePerUnit {
					records = append(records, catalog.PricingRecord{
						Provider:         "aws",
						Product:          "DynamoDB",
						SKU:              sku,
						ProviderSKUID:    sku,
						Region:           product.Attributes["regionCode"],
						PurchaseModel:    "on_demand",
						BillingUnit:      position.Unit,
						PricingDimension: product.Attributes["usagetype"],
						Dimensions: map[string]string{
							"rate_code": rateCode,
							"currency":  currency,
						},
						Price: catalog.Money{Amount: amount, Currency: currency},
						Tier:  tier,
						Source: catalog.Source{
							Type: "aws_price_list",
							URL:  "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonDynamoDB/current/us-east-1/index.json",
						},
					})
				}
			}
		}
	}

	catalog.SortRecords(records)
	return records, nil
}
