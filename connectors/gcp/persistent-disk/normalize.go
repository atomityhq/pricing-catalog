package gcppersistentdisk

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pricing-catalog/pkg/catalog"
)

// normalize filters raw GCP SKUs to the supported Persistent Disk scope and
// transforms each SKU into one or more canonical PricingRecord values.
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

// isSupportedScope retains SKUs belonging to the Persistent Disk domain
// (block storage disks and snapshots) while filtering out Hyperdisk,
// Local SSD, storage pools, and every non-storage family.
//
// Persistent Disk has no dedicated service in the Cloud Billing Catalog API:
// its SKUs share the Compute Engine service (services/6F81-5844-456A) with
// VM, license, networking, Hyperdisk and Local SSD SKUs. The catalog API
// exposes no structured "persistent disk" flag, so the scope decision is
// made from category.resourceFamily plus description markers:
//
//   - the SKU must belong to the Storage family;
//   - Hyperdisk, Local SSD and storage-pool SKUs are excluded by
//     resourceGroup and description;
//   - the description must mention Persistent Disk ("persistent disk" or a
//     standalone "pd" token, e.g. "Storage PD Capacity") or snapshots.
//
// The description wording has not been verified against a live API response
// (fetching the catalog requires a GCP API key); see DESIGN.md for the
// assumption and how to verify it. The rule is deliberately narrow so an
// unexpected description is dropped rather than silently misclassified.
func isSupportedScope(sku gcpSKU) bool {
	family := strings.ToLower(strings.TrimSpace(sku.Category.ResourceFamily))
	if family != "storage" {
		return false
	}

	group := strings.ToLower(strings.TrimSpace(sku.Category.ResourceGroup))
	switch group {
	case "localssd", "hdbsp":
		return false
	}

	desc := strings.ToLower(sku.Description)
	switch {
	case strings.Contains(desc, "hyperdisk"),
		strings.Contains(desc, "local ssd"),
		strings.Contains(desc, "storage pool"):
		return false
	}

	return strings.Contains(desc, "persistent disk") ||
		hasWordToken(desc, "pd") ||
		hasWordToken(desc, "snapshot")
}

// hasWordToken reports whether token appears in s as a standalone word,
// splitting on any character that is not a letter or digit. This avoids
// matching "pd" inside an unrelated word while still matching descriptions
// such as "Storage PD Capacity in Iowa" or "SSD backed PD Capacity".
func hasWordToken(s, token string) bool {
	for _, word := range strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if word == token {
			return true
		}
	}
	return false
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

// isSnapshotSKU reports whether a SKU describes snapshot storage or a
// snapshot operation rather than a disk.
func isSnapshotSKU(sku gcpSKU) bool {
	return hasWordToken(strings.ToLower(sku.Description), "snapshot")
}

// resolveProduct maps a GCP SKU to a human-readable canonical product name.
// Disks and snapshots are separate products because snapshot storage has a
// different billing unit, lifecycle and price from disk capacity.
func resolveProduct(sku gcpSKU) string {
	if isSnapshotSKU(sku) {
		return "Persistent Disk Snapshot"
	}
	return "Persistent Disk"
}

// resolvePurchaseModel maps GCP's usageType and description into a canonical
// purchase model. Persistent Disk has no committed-use or spot pricing on
// the public pricing page, so every SKU is expected to resolve to
// on_demand; the committed/preemptible branches are kept defensively for
// any usageType the API may return.
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

// resolvePricingDimension determines the primary resource dimension being
// priced: the disk type for disks, the snapshot kind for snapshots.
func resolvePricingDimension(sku gcpSKU) string {
	desc := strings.ToLower(sku.Description)

	if isSnapshotSKU(sku) {
		switch {
		case strings.Contains(desc, "archive"):
			return "archive_snapshot"
		case strings.Contains(desc, "instant"):
			return "instant_snapshot"
		default:
			return "standard_snapshot"
		}
	}

	switch {
	case strings.Contains(desc, "extreme"):
		return "extreme"
	case strings.Contains(desc, "balanced"):
		return "balanced"
	case strings.Contains(desc, "ssd"):
		return "ssd"
	default:
		return "standard"
	}
}

// extractDimensions parses commercially meaningful resource distinctions
// from the SKU description: replication (zonal, regional, or multi-regional
// for snapshots), the disk or snapshot type, provisioned IOPS, retrieval
// charges, and any commitment term.
func extractDimensions(sku gcpSKU) map[string]string {
	dims := make(map[string]string)
	desc := strings.ToLower(sku.Description)
	usage := strings.ToLower(sku.Category.UsageType)

	switch {
	case strings.Contains(desc, "multi-region") || strings.Contains(desc, "multi-regional"):
		// Multi-regional snapshots are replicated across regions, not zones.
		dims["replication"] = "multi_regional"
	case isSnapshotSKU(sku) && resolvePricingDimension(sku) == "instant_snapshot":
		// Instant snapshots live in the same zone as their source disk.
		dims["replication"] = "zonal"
	case isSnapshotSKU(sku) || strings.Contains(desc, "regional"):
		// Standard and archive snapshots are stored per region, and
		// regional disks are replicated across two zones.
		dims["replication"] = "regional"
	default:
		dims["replication"] = "zonal"
	}

	if isSnapshotSKU(sku) {
		switch resolvePricingDimension(sku) {
		case "archive_snapshot":
			dims["snapshot_type"] = "archive"
		case "instant_snapshot":
			dims["snapshot_type"] = "instant"
		default:
			dims["snapshot_type"] = "standard"
		}
		if strings.Contains(desc, "retrieval") {
			dims["charge"] = "retrieval"
		}
	} else {
		switch resolvePricingDimension(sku) {
		case "extreme":
			dims["disk_type"] = "pd-extreme"
		case "balanced":
			dims["disk_type"] = "pd-balanced"
		case "ssd":
			dims["disk_type"] = "pd-ssd"
		default:
			dims["disk_type"] = "pd-standard"
		}
		if strings.Contains(desc, "iops") {
			dims["iops"] = "provisioned"
		}
	}

	switch usage {
	case "commit1yr":
		dims["commitment_term"] = "1_year"
	case "commit3yr":
		dims["commitment_term"] = "3_year"
	case "commit1mo":
		dims["commitment_term"] = "1_month"
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

// normalizeBillingUnit converts GCP usageUnit codes (such as "GiBy.mo" or
// "mo") into canonical billing unit names. Unknown units are preserved
// verbatim so provider detail is not silently discarded.
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
