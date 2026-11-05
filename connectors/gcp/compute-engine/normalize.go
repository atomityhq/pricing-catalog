package gcpcomputeengine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pricing-catalog/pkg/catalog"
)

// normalize filters raw GCP SKUs to the supported Compute Engine VM scope
// and transforms each SKU into one or more canonical PricingRecord values.
func (c *Connector) normalize(skus []gcpSKU) ([]catalog.PricingRecord, error) {
	var records []catalog.PricingRecord

	for _, sku := range skus {
		if !isSupportedScope(sku) {
			continue
		}

		skuRecords, err := c.normalizeSKU(sku)
		if err != nil {
			return nil, fmt.Errorf("sku %q: %w", sku.SKUID, err)
		}
		records = append(records, skuRecords...)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("no canonical pricing records produced")
	}

	return records, nil
}

// isSupportedScope retains SKUs belonging to the Compute Engine VM domain
// (Compute cores/memory/GPUs, OS licenses, and attached Local SSDs) while
// filtering out unrelated networking or standard persistent disk SKUs.
func isSupportedScope(sku gcpSKU) bool {
	family := strings.ToLower(strings.TrimSpace(sku.Category.ResourceFamily))
	group := strings.ToLower(strings.TrimSpace(sku.Category.ResourceGroup))

	switch family {
	case "compute", "license":
		return true
	case "storage":
		return group == "localssd"
	default:
		return false
	}
}

// normalizeSKU unrolls a single GCP SKU across its pricing expressions,
// quantity tiers, and applicable regions into canonical PricingRecord entries.
func (c *Connector) normalizeSKU(sku gcpSKU) ([]catalog.PricingRecord, error) {
	if strings.TrimSpace(sku.SKUID) == "" {
		return nil, fmt.Errorf("missing skuId")
	}
	if len(sku.PricingInfo) == 0 {
		return nil, fmt.Errorf("missing pricingInfo")
	}

	regions := extractUniqueRegions(sku)
	product := resolveProduct(sku)
	purchaseModel := resolvePurchaseModel(sku)
	pricingDimension := resolvePricingDimension(sku)
	baseDimensions := extractDimensions(sku)

	var out []catalog.PricingRecord

	for _, pInfo := range sku.PricingInfo {
		var effectiveFrom *time.Time
		if ts := strings.TrimSpace(pInfo.EffectiveTime); ts != "" {
			parsed, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				return nil, fmt.Errorf("parse effectiveTime %q: %w", ts, err)
			}
			utc := parsed.UTC()
			effectiveFrom = &utc
		}

		expr := pInfo.PricingExpression
		billingUnit := normalizeBillingUnit(expr.UsageUnit)

		if len(expr.TieredRates) == 0 {
			return nil, fmt.Errorf("pricingExpression has no tieredRates")
		}

		for i, rate := range expr.TieredRates {
			amount, err := formatGCPMoney(rate.UnitPrice)
			if err != nil {
				return nil, fmt.Errorf("format unitPrice: %w", err)
			}

			currency := strings.ToUpper(strings.TrimSpace(rate.UnitPrice.CurrencyCode))
			if currency == "" {
				currency = "USD"
			}

			tier, err := buildTier(expr.TieredRates, i)
			if err != nil {
				return nil, fmt.Errorf("build tier %d: %w", i, err)
			}

			for _, region := range regions {
				dims := cloneMap(baseDimensions)
				attrs := buildAttributes(sku, pInfo)

				out = append(out, catalog.PricingRecord{
					Provider:          providerName,
					Product:           product,
					SKU:               sku.SKUID,
					ProviderProductID: c.serviceID,
					ProviderSKUID:     sku.SKUID,
					Region:            region,
					PurchaseModel:     purchaseModel,
					BillingUnit:       billingUnit,
					PricingDimension:  pricingDimension,
					Dimensions:        dims,
					Price: catalog.Money{
						Amount:   amount,
						Currency: currency,
					},
					Tier:          tier,
					EffectiveFrom: effectiveFrom,
					Source: catalog.Source{
						Type: defaultSourceType,
						URL:  defaultCatalogURL,
					},
					Attributes: attrs,
				})
			}
		}
	}

	return out, nil
}

// extractUniqueRegions normalizes and deduplicates the list of regions where
// a SKU is available, falling back to "global" when no region is specified.
func extractUniqueRegions(sku gcpSKU) []string {
	rawRegions := sku.ServiceRegions
	if len(rawRegions) == 0 && len(sku.GeoTaxonomy.Regions) > 0 {
		rawRegions = sku.GeoTaxonomy.Regions
	}
	if len(rawRegions) == 0 {
		return []string{"global"}
	}

	seen := make(map[string]struct{}, len(rawRegions))
	result := make([]string, 0, len(rawRegions))
	for _, r := range rawRegions {
		norm := strings.ToLower(strings.TrimSpace(r))
		if norm == "" {
			continue
		}
		if _, exists := seen[norm]; !exists {
			seen[norm] = struct{}{}
			result = append(result, norm)
		}
	}

	if len(result) == 0 {
		return []string{"global"}
	}
	return result
}

// resolveProduct maps a GCP SKU category to a human-readable canonical product name.
func resolveProduct(sku gcpSKU) string {
	family := strings.ToLower(sku.Category.ResourceFamily)
	group := strings.ToLower(sku.Category.ResourceGroup)

	switch {
	case family == "license":
		return "Compute Engine OS License"
	case group == "gpu":
		return "Compute Engine GPU"
	case group == "localssd":
		return "Compute Engine Local SSD"
	default:
		return "Compute Engine VM"
	}
}

// resolvePurchaseModel maps GCP's usageType and description into a canonical purchase model.
func resolvePurchaseModel(sku gcpSKU) string {
	usage := strings.ToLower(sku.Category.UsageType)
	desc := strings.ToLower(sku.Description)

	switch {
	case strings.HasPrefix(usage, "commit"):
		return "committed"
	case usage == "preemptible":
		if strings.Contains(desc, "spot") {
			return "spot"
		}
		return "preemptible"
	default:
		return "on_demand"
	}
}

// resolvePricingDimension determines the primary resource dimension being priced.
func resolvePricingDimension(sku gcpSKU) string {
	family := strings.ToLower(sku.Category.ResourceFamily)
	group := strings.ToLower(sku.Category.ResourceGroup)
	desc := strings.ToLower(sku.Description)

	switch {
	case family == "license":
		return "license"
	case group == "gpu":
		return "gpu"
	case group == "localssd":
		return "local_ssd"
	case group == "ram" || strings.Contains(desc, "ram") || strings.Contains(desc, "memory"):
		return "memory"
	case group == "cpu" || strings.Contains(desc, "core") || strings.Contains(desc, "cpu"):
		return "vcpu"
	default:
		return "resource"
	}
}

// extractDimensions parses commercially meaningful resource distinctions
// (machine family, shape type, memory tier, commitment term, GPU model, OS)
// from the SKU category and description.
func extractDimensions(sku gcpSKU) map[string]string {
	dims := make(map[string]string)
	desc := strings.ToLower(sku.Description)
	group := strings.ToLower(sku.Category.ResourceGroup)
	usage := strings.ToLower(sku.Category.UsageType)

	families := []string{"n2d", "n2", "n1", "e2", "c3d", "c3", "c2d", "c2", "m3", "m2", "m1", "a3", "a2", "g2", "t2d", "t2a"}
	for _, fam := range families {
		if strings.Contains(desc, fam+" ") || strings.HasPrefix(desc, fam+" ") || strings.HasPrefix(group, fam) {
			dims["machine_family"] = fam
			break
		}
	}

	if strings.Contains(desc, "custom") || strings.Contains(group, "custom") {
		dims["shape_type"] = "custom"
		if strings.Contains(desc, "extended") {
			dims["memory_tier"] = "extended"
		} else {
			dims["memory_tier"] = "standard"
		}
	} else if dims["machine_family"] != "" {
		dims["shape_type"] = "predefined"
	}

	switch usage {
	case "commit1yr":
		dims["commitment_term"] = "1_year"
	case "commit3yr":
		dims["commitment_term"] = "3_year"
	case "commit1mo":
		dims["commitment_term"] = "1_month"
	}

	if usage == "preemptible" {
		if strings.Contains(desc, "spot") {
			dims["provisioning_model"] = "spot"
		} else {
			dims["provisioning_model"] = "preemptible"
		}
	}

	if group == "gpu" {
		switch {
		case strings.Contains(desc, "h100"):
			dims["gpu_model"] = "nvidia-h100"
		case strings.Contains(desc, "a100"):
			dims["gpu_model"] = "nvidia-tesla-a100"
		case strings.Contains(desc, "l4"):
			dims["gpu_model"] = "nvidia-l4"
		case strings.Contains(desc, "t4"):
			dims["gpu_model"] = "nvidia-tesla-t4"
		case strings.Contains(desc, "v100"):
			dims["gpu_model"] = "nvidia-tesla-v100"
		}
	}

	if strings.ToLower(sku.Category.ResourceFamily) == "license" {
		switch {
		case strings.Contains(desc, "rhel") || strings.Contains(desc, "red hat"):
			dims["os"] = "rhel"
		case strings.Contains(desc, "windows"):
			dims["os"] = "windows-server"
		case strings.Contains(desc, "sles") || strings.Contains(desc, "suse"):
			dims["os"] = "sles"
		}
	}

	return dims
}

// buildAttributes retains provider-specific metadata on the canonical record.
func buildAttributes(sku gcpSKU, pInfo gcpPricingInfo) map[string]string {
	attrs := map[string]string{
		"description":     sku.Description,
		"resource_family": sku.Category.ResourceFamily,
		"resource_group":  sku.Category.ResourceGroup,
		"usage_type":      sku.Category.UsageType,
	}
	if sku.ServiceProviderName != "" {
		attrs["service_provider_name"] = sku.ServiceProviderName
	}
	if sku.GeoTaxonomy.Type != "" {
		attrs["geo_taxonomy_type"] = string(sku.GeoTaxonomy.Type)
	}
	if pInfo.PricingExpression.BaseUnit != "" {
		attrs["base_unit"] = pInfo.PricingExpression.BaseUnit
	}
	if pInfo.AggregationInfo.AggregationInterval != "" {
		attrs["aggregation_interval"] = string(pInfo.AggregationInfo.AggregationInterval)
	}
	if pInfo.AggregationInfo.AggregationLevel != "" {
		attrs["aggregation_level"] = string(pInfo.AggregationInfo.AggregationLevel)
	}
	return attrs
}

// normalizeBillingUnit converts GCP usageUnit codes (such as "h" or "GiBy.h")
// into canonical billing unit names.
func normalizeBillingUnit(unit string) string {
	switch strings.TrimSpace(unit) {
	case "h":
		return "hour"
	case "GiBy.h":
		return "GiB-hour"
	case "GiBy.mo":
		return "GiB-month"
	case "GiBy.d":
		return "GiB-day"
	case "mo":
		return "month"
	case "d":
		return "day"
	default:
		u := strings.TrimSpace(unit)
		if u == "" {
			return "unit"
		}
		return u
	}
}

// formatGCPMoney converts whole units and nano fractions (10^-9) into an exact
// decimal string without floating-point rounding.
func formatGCPMoney(m gcpMoney) (string, error) {
	unitsStr := strings.TrimSpace(m.Units)
	if unitsStr == "" {
		unitsStr = "0"
	}

	units, err := strconv.ParseInt(unitsStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid units %q: %w", m.Units, err)
	}
	if units < 0 || m.Nanos < 0 || m.Nanos >= 1_000_000_000 {
		return "", fmt.Errorf("negative or out-of-bounds price: units=%d nanos=%d", units, m.Nanos)
	}

	if m.Nanos == 0 {
		return fmt.Sprintf("%d.0000", units), nil
	}

	frac := fmt.Sprintf("%09d", m.Nanos)
	frac = strings.TrimRight(frac, "0")
	if len(frac) < 4 {
		frac += strings.Repeat("0", 4-len(frac))
	}

	return fmt.Sprintf("%d.%s", units, frac), nil
}

// buildTier constructs a canonical Tier interval [StartQuantity, EndQuantity)
// from a slice of GCP tiered rates.
func buildTier(rates []gcpTierRate, idx int) (*catalog.Tier, error) {
	startStr, err := normalizeQuantity(rates[idx].StartUsageAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid startUsageAmount: %w", err)
	}

	var endStr string
	if idx+1 < len(rates) {
		endStr, err = normalizeQuantity(rates[idx+1].StartUsageAmount)
		if err != nil {
			return nil, fmt.Errorf("invalid next startUsageAmount: %w", err)
		}
	}

	return &catalog.Tier{
		StartQuantity: startStr,
		EndQuantity:   endStr,
	}, nil
}

// normalizeQuantity validates and formats a json.Number quantity threshold.
func normalizeQuantity(num json.Number) (string, error) {
	raw := strings.TrimSpace(num.String())
	if raw == "" {
		return "0", nil
	}
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil || val < 0 {
		return "", fmt.Errorf("invalid non-negative quantity %q", raw)
	}
	return raw, nil
}

// cloneMap returns a shallow copy of a string map so distinct PricingRecords
// do not share mutable map references.
func cloneMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
