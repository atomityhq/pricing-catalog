package azuremanageddisks

import (
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"pricing-catalog/pkg/catalog"
)

// diskProducts maps the Azure Retail Prices productName prefixes that belong
// to the Managed Disks pricing domain to a short disk type name.
//
// Prefix matching is deliberate: Azure publishes some price lists under
// versioned product names such as "Premium SSD Managed Disks_v8". Everything
// else returned by the source (blobs, files, page blobs, ...) is out of scope.
var diskProducts = []struct {
	prefix   string
	diskType string
}{
	{"Standard HDD Managed Disks", "standard_hdd"},
	{"Standard SSD Managed Disks", "standard_ssd"},
	{"Premium SSD Managed Disks", "premium_ssd"},
	{"Azure Premium SSD v2", "premium_ssd_v2"},
	{"Ultra Disks", "ultra"},
}

func diskTypeOf(productName string) (string, bool) {
	for _, p := range diskProducts {
		if strings.HasPrefix(productName, p.prefix) {
			return p.diskType, true
		}
	}
	return "", false
}

// meterComponents maps a phrase in the meter name to the component being
// priced. Order matters: the first match wins, so more specific phrases come
// before general ones ("capacity restored" before "instant access snapshots",
// which comes before "snapshots").
var meterComponents = []struct {
	phrase    string
	dimension string
}{
	{"confidential compute encryption", "confidential_compute_encryption"},
	{"capacity restored", "instant_access_snapshot_restore"},
	{"instant access snapshots", "instant_access_snapshot"},
	{"snapshots", "snapshot"},
	{"burst enablement", "burst_enablement"},
	{"burst transactions", "burst_transactions"},
	{"disk operations", "transactions"},
	{"disk mount", "disk_mount"},
	{"provisioned capacity", "provisioned_capacity"},
	{"provisioned iops", "provisioned_iops"},
	{"provisioned throughput", "provisioned_throughput"},
	{"reservation per vcpu", "vcpu_reservation"},
}

var (
	diskTierPattern = regexp.MustCompile(`^([PES][0-9]+)\b`)
	reservationTerm = regexp.MustCompile(`^([0-9]+) Years?$`)
	nonSlugChars    = regexp.MustCompile(`[^a-z0-9]+`)
)

// classifyMeter returns the canonical pricing dimension of a meter and
// whether the meter is a free allowance ("... - Free").
func classifyMeter(meterName string) (dimension string, free bool) {
	name := strings.TrimSpace(meterName)
	if strings.HasSuffix(name, " - Free") {
		free = true
		name = strings.TrimSuffix(name, " - Free")
	}
	lower := strings.ToLower(name)

	for _, c := range meterComponents {
		if strings.Contains(lower, c.phrase) {
			return c.dimension, free
		}
	}

	// "P30 LRS Disk", "E4 Disks": the provisioned disk itself.
	if strings.HasSuffix(lower, " disk") || strings.HasSuffix(lower, " disks") {
		return "disk", free
	}

	// Unknown meter: keep it, under a stable name derived from the meter,
	// rather than dropping a published price.
	slug := strings.Trim(nonSlugChars.ReplaceAllString(lower, "_"), "_")
	return "meter_" + slug, free
}

// diskSizeGiB is the provisioned size of each fixed-size disk tier, as
// documented at https://learn.microsoft.com/en-us/azure/virtual-machines/disks-types
// (P = Premium SSD, E = Standard SSD, S = Standard HDD share the same sizes).
// The Retail Prices API does not publish sizes, so unknown future tiers are
// simply left without a size rather than guessed.
var diskSizeGiB = map[string]string{
	"1": "4", "2": "8", "3": "16", "4": "32", "6": "64", "10": "128", "15": "256",
	"20": "512", "30": "1024", "40": "2048", "50": "4096", "60": "8192",
	"70": "16384", "80": "32767",
}

func buildDimensions(item retailPriceItem, diskType string, free bool, term string) map[string]string {
	dims := map[string]string{"disk_type": diskType}

	if redundancy := redundancyOf(item.SKUName); redundancy != "" {
		dims["redundancy"] = redundancy
	}

	// "P30 LRS" -> disk_tier "P30", disk_size_gib "1024".
	if m := diskTierPattern.FindStringSubmatch(item.SKUName); m != nil {
		tier := m[1]
		dims["disk_tier"] = tier
		if size, ok := diskSizeGiB[tier[1:]]; ok {
			dims["disk_size_gib"] = size
		}
	}

	if term != "" {
		dims["commitment_term"] = term
	}
	if free {
		dims["offer"] = "free"
	}

	return dims
}

func redundancyOf(skuName string) string {
	for _, field := range strings.Fields(skuName) {
		switch field {
		case "LRS":
			return "lrs"
		case "ZRS":
			return "zrs"
		}
	}
	return ""
}

// purchaseModelOf maps Azure's price "type" to a canonical purchase model.
// Unknown types are an error: guessing would mislabel a commercial offer.
func purchaseModelOf(priceType string) (string, error) {
	switch priceType {
	case "Consumption":
		return "on_demand", nil
	case "Reservation":
		return "reserved", nil
	default:
		return "", fmt.Errorf("unsupported price type %q", priceType)
	}
}

// parseReservationTerm turns "1 Year" / "3 Years" into ("1_year", "1-year").
func parseReservationTerm(raw string) (dimension, billingUnit string, err error) {
	m := reservationTerm.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return "", "", fmt.Errorf("unsupported reservation term %q", raw)
	}
	return m[1] + "_year", m[1] + "-year", nil
}

// billingUnitOf normalizes Azure's unitOfMeasure.
//
//	"1/Month"    -> "month"     (per item, e.g. per disk, per month)
//	"1/Hour"     -> "hour"      (per provisioned IOPS / MBps / vCPU, per hour)
//	"1 GB/Month" -> "GB-month"
//	"1 GiB/Hour" -> "GiB-hour"
//	"1 TB"       -> "TB"
//	"10K"        -> "10K"       (per 10,000 operations)
//	"10K/Month"  -> "10K-month"
//
// The raw value is always kept in Attributes["unit_of_measure"].
func billingUnitOf(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("unitOfMeasure is empty")
	}

	quantityAndUnit, period, _ := strings.Cut(value, "/")
	fields := strings.Fields(quantityAndUnit)

	var parts []string
	for i, field := range fields {
		if i == 0 && field == "1" {
			continue
		}
		parts = append(parts, field)
	}
	if period = strings.TrimSpace(period); period != "" {
		parts = append(parts, strings.ToLower(period))
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("unsupported unitOfMeasure %q", raw)
	}
	return strings.Join(parts, "-"), nil
}

// tierGroup identifies all tiers of one published price: same SKU, meter,
// region, purchase option, currency, unit and validity window.
type tierGroup struct {
	sku, meterID, region, priceType, term, unit, currency, from, to string
}

func tierGroupOf(item retailPriceItem) tierGroup {
	return tierGroup{
		sku:       item.SKUID,
		meterID:   item.MeterID,
		region:    item.ARMRegionName,
		priceType: item.Type,
		term:      item.ReservationTerm,
		unit:      item.UnitOfMeasure,
		currency:  item.CurrencyCode,
		from:      item.EffectiveStartDate,
		to:        item.EffectiveEndDate,
	}
}

// normalize converts in-scope Azure price items into canonical records.
//
// Numbers are never rewritten: prices and tier bounds keep the exact text
// Azure published (for example "1695.0" and "3000.0"). The generic pipeline
// validates their format.
func normalize(items []retailPriceItem, sourceType, sourceURL string) ([]catalog.PricingRecord, error) {
	type row struct {
		record  catalog.PricingRecord
		start   string   // tierMinimumUnits exactly as Azure wrote it
		minimum *big.Rat // the same value as a number, used only for sorting
	}

	// Azure publishes each price tier of a meter as a separate item that only
	// carries its lower bound (tierMinimumUnits). Collect the items of each
	// meter first, then build the tiers.
	groups := make(map[tierGroup][]row)
	var order []tierGroup

	for i, item := range items {
		diskType, ok := diskTypeOf(item.ProductName)
		if !ok {
			continue // not a Managed Disks product
		}

		record, err := normalizeItem(item, diskType, sourceType, sourceURL)
		if err != nil {
			return nil, fmt.Errorf("item %d (product %q, sku %q, meter %q, region %q): %w",
				i, item.ProductName, item.SKUID, item.MeterName, item.ARMRegionName, err)
		}

		start := item.TierMinimumUnits.String()
		minimum, ok := new(big.Rat).SetString(start)
		if !ok {
			return nil, fmt.Errorf("item %d (sku %q, meter %q): invalid tierMinimumUnits %q",
				i, item.SKUID, item.MeterName, start)
		}

		key := tierGroupOf(item)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], row{record: record, start: start, minimum: minimum})
	}

	records := make([]catalog.PricingRecord, 0, len(items))
	for _, key := range order {
		rows := groups[key]
		sort.SliceStable(rows, func(a, b int) bool {
			return rows[a].minimum.Cmp(rows[b].minimum) < 0
		})

		// A single item starting at zero is a flat price, not a tier.
		if len(rows) == 1 && rows[0].minimum.Sign() == 0 {
			records = append(records, rows[0].record)
			continue
		}

		// Each tier ends where the next one starts; the last one has no end.
		for i, r := range rows {
			tier := &catalog.Tier{StartQuantity: r.start}
			if i+1 < len(rows) {
				next := rows[i+1]
				if next.minimum.Cmp(r.minimum) == 0 {
					return nil, fmt.Errorf("meter %q (sku %q, region %q): duplicate tier starting at %s",
						r.record.Attributes["meter_name"], r.record.SKU, r.record.Region, r.start)
				}
				tier.EndQuantity = next.start
			}
			r.record.Tier = tier
			records = append(records, r.record)
		}
	}

	return records, nil
}

func normalizeItem(item retailPriceItem, diskType, sourceType, sourceURL string) (catalog.PricingRecord, error) {
	required := []struct{ field, value string }{
		{"skuId", item.SKUID},
		{"productId", item.ProductID},
		{"meterId", item.MeterID},
		{"meterName", item.MeterName},
		{"armRegionName", item.ARMRegionName},
		{"type", item.Type},
	}
	for _, r := range required {
		if strings.TrimSpace(r.value) == "" {
			return catalog.PricingRecord{}, fmt.Errorf("%s is required", r.field)
		}
	}

	purchaseModel, err := purchaseModelOf(item.Type)
	if err != nil {
		return catalog.PricingRecord{}, err
	}

	billingUnit, err := billingUnitOf(item.UnitOfMeasure)
	if err != nil {
		return catalog.PricingRecord{}, err
	}

	var term string
	if purchaseModel == "reserved" {
		// Azure publishes reservation prices as the total price of one
		// reserved unit for the whole term, even though unitOfMeasure still
		// reads "1/Month". Reflect what the amount really is.
		term, billingUnit, err = parseReservationTerm(item.ReservationTerm)
		if err != nil {
			return catalog.PricingRecord{}, err
		}
	}

	effectiveFrom, err := parseTime(item.EffectiveStartDate)
	if err != nil {
		return catalog.PricingRecord{}, fmt.Errorf("effectiveStartDate: %w", err)
	}
	effectiveTo, err := parseTime(item.EffectiveEndDate)
	if err != nil {
		return catalog.PricingRecord{}, fmt.Errorf("effectiveEndDate: %w", err)
	}

	dimension, free := classifyMeter(item.MeterName)

	return catalog.PricingRecord{
		Provider:          providerName,
		Product:           strings.TrimSpace(item.ProductName),
		SKU:               item.SKUID,
		ProviderProductID: item.ProductID,
		ProviderSKUID:     item.SKUID,
		Region:            strings.ToLower(strings.TrimSpace(item.ARMRegionName)),
		PurchaseModel:     purchaseModel,
		BillingUnit:       billingUnit,
		PricingDimension:  dimension,
		Dimensions:        buildDimensions(item, diskType, free, term),
		Price:             catalog.Money{Amount: item.RetailPrice.String(), Currency: item.CurrencyCode},
		EffectiveFrom:     effectiveFrom,
		EffectiveTo:       effectiveTo,
		Source:            catalog.Source{Type: sourceType, URL: sourceURL},
		Attributes:        buildAttributes(item, purchaseModel),
	}, nil
}

func buildAttributes(item retailPriceItem, purchaseModel string) map[string]string {
	attrs := map[string]string{
		"meter_id":        item.MeterID,
		"meter_name":      item.MeterName,
		"sku_name":        item.SKUName,
		"price_type":      item.Type,
		"unit_of_measure": item.UnitOfMeasure,
	}

	optional := map[string]string{
		"arm_sku_name":     item.ARMSKUName,
		"location":         item.Location,
		"service_name":     item.ServiceName,
		"service_id":       item.ServiceID,
		"service_family":   item.ServiceFamily,
		"reservation_term": item.ReservationTerm,
	}
	for key, value := range optional {
		if strings.TrimSpace(value) != "" {
			attrs[key] = value
		}
	}

	if item.IsPrimaryMeterRegion != nil {
		attrs["is_primary_meter_region"] = strconv.FormatBool(*item.IsPrimaryMeterRegion)
	}
	if purchaseModel == "reserved" {
		attrs["price_basis"] = "total_for_term"
	}

	return attrs
}

func parseTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	t = t.UTC()
	return &t, nil
}
