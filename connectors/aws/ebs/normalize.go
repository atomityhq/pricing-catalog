package aws_ebs

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"pricing-catalog/pkg/catalog"
)

type AwsData struct {
	FormatVersion string                                   `json:"formatVersion"`
	OfferCode     string                                   `json:"offerCode"`
	Version       string                                   `json:"version"`
	Products      map[string]AwsProduct                    `json:"products"`
	Terms         map[string]map[string]map[string]AwsTerm `json:"terms"`
}

type AwsProduct struct {
	Sku           string            `json:"sku"`
	ProductFamily string            `json:"productFamily"`
	Attributes    map[string]string `json:"attributes"`
}

type AwsTerm struct {
	Sku             string                       `json:"sku"`
	OfferTermCode   string                       `json:"offerTermCode"`
	EffectiveDate   string                       `json:"effectiveDate"`
	PriceDimensions map[string]AwsPriceDimension `json:"priceDimensions"`
	TermAttributes  map[string]string            `json:"termAttributes"`
}

type AwsPriceDimension struct {
	RateCode     string            `json:"rateCode"`
	Description  string            `json:"description"`
	BeginRange   string            `json:"beginRange"`
	EndRange     string            `json:"endRange"`
	Unit         string            `json:"unit"`
	PricePerUnit map[string]string `json:"pricePerUnit"`
	AppliesTo    []string          `json:"appliesTo"`
}

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func normalizeData(data []byte) ([]catalog.PricingRecord, error) {
	var offer AwsData
	if err := json.Unmarshal(data, &offer); err != nil {
		return nil, fmt.Errorf("decode AWS offer JSON: %w", err)
	}
	if err := validateOffer(offer); err != nil {
		return nil, err
	}

	records := make([]catalog.PricingRecord, 0)
	for _, sku := range sortedKeys(offer.Products) {
		product := offer.Products[sku]
		if product.Sku != sku || strings.TrimSpace(sku) == "" {
			return nil, fmt.Errorf("invalid product SKU: %q", sku)
		}
		usage := product.Attributes["usagetype"]
		if strings.TrimSpace(usage) == "" {
			return nil, fmt.Errorf("SKU %s: attributes.usagetype is required", sku)
		}
		if !strings.HasPrefix(usage, "EBS:") && !strings.Contains(usage, "-EBS:") {
			continue
		}

		productRecords, err := normalizeProduct(offer, sku, product)
		if err != nil {
			return nil, fmt.Errorf("SKU %s: %w", sku, err)
		}
		records = append(records, productRecords...)
	}
	return records, nil
}

func validateOffer(offer AwsData) error {
	if offer.FormatVersion != "v1.0" {
		return fmt.Errorf("unsupported format version: %q", offer.FormatVersion)
	}
	if offer.OfferCode != "AmazonEC2" {
		return fmt.Errorf("unsupported offer code: %q", offer.OfferCode)
	}
	if strings.TrimSpace(offer.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if len(offer.Products) == 0 {
		return fmt.Errorf("products must not be empty")
	}
	if offer.Terms == nil {
		return fmt.Errorf("terms is required")
	}
	if offer.Terms["OnDemand"] == nil {
		return fmt.Errorf("terms.OnDemand is required")
	}
	return nil
}

func normalizeProduct(offer AwsData, sku string, product AwsProduct) ([]catalog.PricingRecord, error) {
	if product.Attributes["servicecode"] != "AmazonEC2" {
		return nil, fmt.Errorf("attributes.servicecode must be AmazonEC2")
	}
	if strings.TrimSpace(product.ProductFamily) == "" {
		return nil, fmt.Errorf("productFamily is required")
	}
	if strings.TrimSpace(product.Attributes["regionCode"]) == "" {
		return nil, fmt.Errorf("attributes.regionCode is required")
	}
	for _, model := range sortedKeys(offer.Terms) {
		if _, exists := offer.Terms[model][sku]; exists && model != "OnDemand" {
			return nil, fmt.Errorf("unsupported EBS purchase model: %s", model)
		}
	}
	terms := offer.Terms["OnDemand"][sku]
	if len(terms) == 0 {
		return nil, fmt.Errorf("no OnDemand terms found")
	}

	var records []catalog.PricingRecord
	for _, termKey := range sortedKeys(terms) {
		term := terms[termKey]
		if term.Sku != sku || strings.TrimSpace(term.OfferTermCode) == "" ||
			termKey != sku+"."+term.OfferTermCode || len(term.PriceDimensions) == 0 {
			return nil, fmt.Errorf("invalid EBS term: %s", termKey)
		}
		effective, err := time.Parse(time.RFC3339, term.EffectiveDate)
		if err != nil {
			return nil, fmt.Errorf("term %s: invalid effectiveDate %q: %w", termKey, term.EffectiveDate, err)
		}
		effective = effective.UTC()

		for _, rateCode := range sortedKeys(term.PriceDimensions) {
			price := term.PriceDimensions[rateCode]
			end, err := validatePriceDimension(termKey, rateCode, price)
			if err != nil {
				return nil, fmt.Errorf("rate %s: %w", rateCode, err)
			}
			for _, currency := range sortedKeys(price.PricePerUnit) {
				amount := price.PricePerUnit[currency]
				if currency != "USD" {
					return nil, fmt.Errorf("rate %s: unsupported currency: %s", rateCode, currency)
				}
				if !decimalPattern.MatchString(amount) {
					return nil, fmt.Errorf("rate %s: invalid price amount: %q", rateCode, amount)
				}
				records = append(records, buildRecord(offer, product, term, price, currency, end, effective))
			}
		}
	}
	return records, nil
}

func validatePriceDimension(termKey, rateCode string, price AwsPriceDimension) (string, error) {
	if price.RateCode != rateCode || !strings.HasPrefix(rateCode, termKey+".") || rateCode == termKey+"." {
		return "", fmt.Errorf("invalid rateCode")
	}
	if strings.TrimSpace(price.Description) == "" {
		return "", fmt.Errorf("description is required")
	}
	if strings.TrimSpace(price.Unit) == "" {
		return "", fmt.Errorf("unit is required")
	}
	if len(price.PricePerUnit) == 0 {
		return "", fmt.Errorf("pricePerUnit must not be empty")
	}
	if !decimalPattern.MatchString(price.BeginRange) {
		return "", fmt.Errorf("invalid beginRange: %q", price.BeginRange)
	}
	if price.EndRange == "Inf" {
		return "", nil
	}
	if !decimalPattern.MatchString(price.EndRange) {
		return "", fmt.Errorf("invalid endRange: %q", price.EndRange)
	}

	// Compare exact decimal quantities without floating-point rounding.
	start, _ := new(big.Rat).SetString(price.BeginRange)
	end, _ := new(big.Rat).SetString(price.EndRange)
	if start.Cmp(end) >= 0 {
		return "", fmt.Errorf("invalid EBS price range: %s - %s", price.BeginRange, price.EndRange)
	}
	return price.EndRange, nil
}

func buildRecord(offer AwsData, product AwsProduct, term AwsTerm, price AwsPriceDimension,
	currency, end string, effective time.Time,
) catalog.PricingRecord {
	dimensions := map[string]string{
		"rateCode":   price.RateCode,
		"beginRange": price.BeginRange,
		"endRange":   end,
		"unit":       price.Unit,
	}
	for _, key := range []string{"usagetype", "volumeApiName", "volumeType", "storageMedia", "operation", "location", "locationType"} {
		if value := product.Attributes[key]; value != "" {
			dimensions[key] = value
		}
	}
	for key, value := range term.TermAttributes {
		dimensions["term_attribute."+key] = value
	}
	if len(price.AppliesTo) > 0 {
		conditions, _ := json.Marshal(price.AppliesTo) // A []string can always be marshaled.
		dimensions["applies_to"] = string(conditions)
	}

	attributes := make(map[string]string, len(product.Attributes)+3)
	for key, value := range product.Attributes {
		attributes[key] = value
	}
	attributes["description"] = price.Description
	attributes["product_family"] = product.ProductFamily
	attributes["offer_version"] = offer.Version

	region := product.Attributes["regionCode"]
	return catalog.PricingRecord{
		Provider:          "aws",
		Product:           "Amazon EBS",
		SKU:               product.Sku,
		ProviderProductID: "AmazonEC2",
		ProviderSKUID:     product.Sku,
		Region:            region,
		PurchaseModel:     "on_demand",
		BillingUnit:       price.Unit,
		PricingDimension:  product.Attributes["usagetype"],
		Dimensions:        dimensions,
		Price:             catalog.Money{Amount: price.PricePerUnit[currency], Currency: currency},
		Tier:              &catalog.Tier{StartQuantity: price.BeginRange, EndQuantity: end},
		EffectiveFrom:     &effective,
		Source: catalog.Source{
			Type: "aws-price-list-json",
			URL: "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonEC2/" +
				url.PathEscape(offer.Version) + "/" + url.PathEscape(region) + "/index.json",
		},
		Attributes: attributes,
	}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
