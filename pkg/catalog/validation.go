package catalog

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// ValidateRecord validates one canonical pricing record.
func ValidateRecord(r PricingRecord) error {
	checks := []struct {
		field string
		value string
	}{
		{"provider", r.Provider},
		{"product", r.Product},
		{"sku", r.SKU},
		{"region", r.Region},
		{"purchase_model", r.PurchaseModel},
		{"billing_unit", r.BillingUnit},
		{"pricing_dimension", r.PricingDimension},
		{"price.amount", r.Price.Amount},
		{"price.currency", r.Price.Currency},
		{"source.type", r.Source.Type},
		{"source.url", r.Source.URL},
	}

	for _, check := range checks {
		field, value := check.field, check.value
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}

	if !decimalPattern.MatchString(r.Price.Amount) {
		return fmt.Errorf("price.amount must be a non-negative decimal string, got %q", r.Price.Amount)
	}

	if !currencyPattern.MatchString(r.Price.Currency) {
		return fmt.Errorf("price.currency must be an ISO-like 3-letter uppercase code, got %q", r.Price.Currency)
	}

	if _, err := url.ParseRequestURI(r.Source.URL); err != nil {
		return fmt.Errorf("source.url is invalid: %w", err)
	}

	if r.Tier != nil {
		if !decimalPattern.MatchString(r.Tier.StartQuantity) {
			return fmt.Errorf("tier.start_quantity must be a decimal string, got %q", r.Tier.StartQuantity)
		}
		if r.Tier.EndQuantity != "" && !decimalPattern.MatchString(r.Tier.EndQuantity) {
			return fmt.Errorf("tier.end_quantity must be a decimal string, got %q", r.Tier.EndQuantity)
		}
	}

	if r.EffectiveFrom != nil && r.EffectiveTo != nil && r.EffectiveFrom.After(*r.EffectiveTo) {
		return fmt.Errorf("effective_from must not be after effective_to")
	}

	return nil
}

// ValidateRecords validates all records and rejects duplicate canonical keys.
func ValidateRecords(records []PricingRecord) error {
	seen := make(map[string]struct{}, len(records))

	for i, record := range records {
		if err := ValidateRecord(record); err != nil {
			return fmt.Errorf("record %d: %w", i, err)
		}

		key := RecordKey(record)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate pricing record: %s", key)
		}
		seen[key] = struct{}{}
	}

	return nil
}

type identity struct {
	Provider          string            `json:"provider"`
	Product           string            `json:"product"`
	SKU               string            `json:"sku"`
	ProviderProductID string            `json:"provider_product_id,omitempty"`
	ProviderSKUID     string            `json:"provider_sku_id,omitempty"`
	Region            string            `json:"region"`
	PurchaseModel     string            `json:"purchase_model"`
	BillingUnit       string            `json:"billing_unit"`
	PricingDimension  string            `json:"pricing_dimension"`
	Dimensions        map[string]string `json:"dimensions,omitempty"`
	Tier              string            `json:"tier,omitempty"`
	EffectiveFrom     string            `json:"effective_from,omitempty"`
	EffectiveTo       string            `json:"effective_to,omitempty"`
}

// RecordKey returns the stable identity of a pricing record within a snapshot.
func RecordKey(r PricingRecord) string {
	key, _ := json.Marshal(identity{
		Provider:          r.Provider,
		Product:           r.Product,
		SKU:               r.SKU,
		ProviderProductID: r.ProviderProductID,
		ProviderSKUID:     r.ProviderSKUID,
		Region:            r.Region,
		PurchaseModel:     r.PurchaseModel,
		BillingUnit:       r.BillingUnit,
		PricingDimension:  r.PricingDimension,
		Dimensions:        r.Dimensions,
		Tier:              tierKey(r.Tier),
		EffectiveFrom:     timeKey(r.EffectiveFrom),
		EffectiveTo:       timeKey(r.EffectiveTo),
	})

	return string(key)
}

func timeKey(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func tierKey(tier *Tier) string {
	if tier == nil {
		return ""
	}
	return tier.StartQuantity + ":" + tier.EndQuantity
}
