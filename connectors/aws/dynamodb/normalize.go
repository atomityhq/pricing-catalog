package dynamodb

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"pricing-catalog/pkg/catalog"
)

const (
	providerName = "aws"
	sourceType   = "aws_price_list"
	sourceURL    = "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonDynamoDB/current/index.json"
	productName  = "Amazon DynamoDB"
)

var nonIdentifier = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// normalizeRecords converts AWS products and terms into canonical pricing records.
// It intentionally emits one record per price dimension and currency: reserved
// recurring charges and upfront fees must remain separate records.
func normalizeRecords(products map[string]sourceProduct, terms []sourceTerm) ([]catalog.PricingRecord, error) {
	var records []catalog.PricingRecord

	for _, term := range terms {
		prod, ok := products[term.SKU]
		if !ok {
			continue
		}

		region, include, err := normalizeRegion(prod)
		if err != nil {
			return nil, fmt.Errorf("SKU %s: %w", term.SKU, err)
		}
		if !include {
			continue
		}

		purchaseModel, err := normalizePurchaseModel(term.TermType)
		if err != nil {
			return nil, fmt.Errorf("SKU %s term %s: %w", term.SKU, term.TermCode, err)
		}

		var effectiveFrom *time.Time
		if term.EffectiveDate != "" {
			t, err := time.Parse(time.RFC3339, term.EffectiveDate)
			if err != nil {
				return nil, fmt.Errorf("SKU %s term %s: invalid effectiveDate %q: %w", term.SKU, term.TermCode, term.EffectiveDate, err)
			}
			tUTC := t.UTC()
			effectiveFrom = &tUTC
		}

		for _, dim := range term.PriceDimensions {
			billingUnit := strings.TrimSpace(dim.Unit)
			if billingUnit == "" {
				return nil, fmt.Errorf("SKU %s term %s rate %s: empty billing unit", term.SKU, term.TermCode, dim.RateCode)
			}

			pricingDimension := classifyPricingDimension(prod, term, dim)
			tier, err := buildTier(dim)
			if err != nil {
				return nil, fmt.Errorf("SKU %s term %s rate %s: %w", term.SKU, term.TermCode, dim.RateCode, err)
			}

			currencies := sortedKeys(dim.PricePerUnit)
			if len(currencies) == 0 {
				return nil, fmt.Errorf("SKU %s term %s rate %s: no currency amounts", term.SKU, term.TermCode, dim.RateCode)
			}

			for _, currency := range currencies {
				amount := strings.TrimSpace(dim.PricePerUnit[currency])
				normalizedCurrency := strings.ToUpper(strings.TrimSpace(currency))
				if amount == "" {
					return nil, fmt.Errorf("SKU %s term %s rate %s currency %s: empty price amount", term.SKU, term.TermCode, dim.RateCode, normalizedCurrency)
				}

				rec := catalog.PricingRecord{
					Provider:          providerName,
					Product:           productName,
					SKU:               term.SKU,
					ProviderProductID: term.SKU,
					ProviderSKUID:     buildProviderSKUID(term, dim, normalizedCurrency),
					Region:            region,
					PurchaseModel:     purchaseModel,
					BillingUnit:       billingUnit,
					PricingDimension:  pricingDimension,
					Dimensions:        buildDimensions(prod),
					Price: catalog.Money{
						Amount:   amount,
						Currency: normalizedCurrency,
					},
					Tier:          tier,
					EffectiveFrom: effectiveFrom,
					Source:        catalog.Source{Type: sourceType, URL: sourceURL},
					Attributes:    buildAttributes(term, dim),
				}

				if err := catalog.ValidateRecord(rec); err != nil {
					return nil, fmt.Errorf("SKU %s term %s rate %s currency %s: %w", term.SKU, term.TermCode, dim.RateCode, normalizedCurrency, err)
				}
				records = append(records, rec)
			}
		}
	}

	catalog.SortRecords(records)
	if err := catalog.ValidateRecords(records); err != nil {
		return nil, fmt.Errorf("validate normalized DynamoDB records: %w", err)
	}
	return records, nil
}

func normalizePurchaseModel(termType string) (string, error) {
	switch termType {
	case "OnDemand":
		return "on_demand", nil
	case "Reserved":
		return "reserved", nil
	default:
		return "", fmt.Errorf("unsupported term type %q", termType)
	}
}

// normalizeRegion preserves AWS region codes. The source has one global vector
// search SKU with no regionCode and two TST test entries, which are not sellable
// regional prices and are handled explicitly rather than guessed.
func normalizeRegion(p sourceProduct) (region string, include bool, err error) {
	region = strings.TrimSpace(p.Attributes["regionCode"])
	if region != "" && !strings.EqualFold(region, "TST") {
		return region, true, nil
	}
	if strings.EqualFold(region, "TST") || strings.EqualFold(p.Attributes["location"], "TST") {
		return "", false, nil
	}
	if region == "" && strings.EqualFold(p.Attributes["location"], "Any") && p.Attributes["usagetype"] == "Global-VectorSearch" {
		return "global", true, nil
	}
	return "", false, fmt.Errorf("missing or unsupported regionCode (location=%q usageType=%q)", p.Attributes["location"], p.Attributes["usagetype"])
}

func classifyPricingDimension(p sourceProduct, term sourceTerm, dim sourcePriceDimension) string {
	a := p.Attributes
	family := strings.ToLower(p.ProductFamily)
	group := strings.ToLower(a["group"])
	usage := strings.ToLower(a["usagetype"])
	description := strings.ToLower(dim.Description)
	unit := strings.ToLower(dim.Unit)

	if strings.Contains(description, "upfront") ||
		(strings.EqualFold(term.TermType, "Reserved") && unit == "quantity") {
		return "upfront_fee"
	}

	switch {
	case group == "ddb-kinesis" || group == "ddb-elasticviews" || strings.Contains(usage, "changedatacapture"):
		return "change_data_capture"
	case group == "ddb-streamsreadrequests":
		return "streams_read_request"
	case strings.Contains(family, "import data size") || strings.Contains(usage, "bulkput"):
		return "import_data"
	case strings.Contains(family, "export data size") || strings.Contains(usage, "exportdatasize"):
		return "export_data"
	case strings.Contains(family, "restore data size") || strings.Contains(usage, "restoredata"):
		return "restore_data"
	case strings.Contains(family, "on-demand backup storage") || strings.Contains(usage, "timedbackupstorage"):
		return "backup_storage"
	case strings.Contains(a["volumeType"], "Point-In-Time-Restore") || strings.Contains(usage, "timedpitrstorage"):
		return "pitr_backup_storage"
	case strings.Contains(family, "database storage"):
		if strings.Contains(strings.ToLower(a["volumeType"]), "indexed datastore - ia") || strings.Contains(usage, "ia-timedstorage") {
			return "indexed_storage_ia"
		}
		return "indexed_storage"
	case strings.Contains(family, "provisioned iops"):
		if strings.Contains(group, "readunits") {
			return "read_capacity"
		}
		if strings.Contains(group, "writeunits") {
			return "write_capacity"
		}
	case strings.Contains(family, "replicatedwrite"):
		return "replicated_write_capacity"
	}

	switch {
	case strings.Contains(group, "vectorsearch") || strings.Contains(usage, "vectorsearch"):
		return "vector_search"
	case strings.Contains(group, "vectorwriterequest") || strings.Contains(usage, "vectorwriterequest"):
		return "vector_write"
	case strings.Contains(group, "replicatedwrite") || strings.Contains(usage, "replwrite"):
		return "replicated_write_request"
	case strings.Contains(group, "readunits") || strings.Contains(usage, "readrequestunits"):
		return "read_request"
	case strings.Contains(group, "writeunits") || strings.Contains(usage, "writerequestunits"):
		return "write_request"
	case strings.Contains(usage, "readcapacityunit"):
		return "read_capacity"
	case strings.Contains(usage, "writecapacityunit"):
		return "write_capacity"
	case strings.Contains(usage, "storage") || strings.Contains(unit, "byte") || strings.Contains(unit, "gb"):
		return "storage"
	default:
		return "dynamodb_usage"
	}
}

func buildTier(dim sourcePriceDimension) (*catalog.Tier, error) {
	begin := strings.TrimSpace(dim.BeginRange)
	end := strings.TrimSpace(dim.EndRange)
	if begin == "" {
		begin = "0"
	}
	if end == "" {
		end = "Inf"
	}
	if begin == "0" && strings.EqualFold(end, "Inf") {
		return nil, nil
	}
	if strings.EqualFold(end, "Inf") {
		end = ""
	}
	return &catalog.Tier{StartQuantity: begin, EndQuantity: end}, nil
}

func buildProviderSKUID(term sourceTerm, dim sourcePriceDimension, currency string) string {
	return strings.Join([]string{term.TermCode, term.OfferTermCode, dim.RateCode, currency}, "|")
}

// buildDimensions retains the full AWS product attribute set. Keys are converted
// to snake_case for stable canonical field names; values remain source-exact.
func buildDimensions(p sourceProduct) map[string]string {
	if len(p.Attributes) == 0 {
		return nil
	}
	keys := sortedKeys(p.Attributes)
	dimensions := make(map[string]string, len(p.Attributes)+1)
	if strings.TrimSpace(p.ProductFamily) != "" {
		dimensions["product_family"] = strings.TrimSpace(p.ProductFamily)
	}
	for _, key := range keys {
		value := strings.TrimSpace(p.Attributes[key])
		if value == "" {
			continue
		}
		dimensions[toSnakeCase(key)] = value
	}
	if len(dimensions) == 0 {
		return nil
	}
	return dimensions
}

func buildAttributes(term sourceTerm, dim sourcePriceDimension) map[string]string {
	attributes := map[string]string{
		"term_type":       term.TermType,
		"term_code":       term.TermCode,
		"offer_term_code": term.OfferTermCode,
		"rate_code":       dim.RateCode,
		"description":     dim.Description,
		"source_unit":     dim.Unit,
		"begin_range":     dim.BeginRange,
		"end_range":       dim.EndRange,
	}
	for key, value := range term.TermAttributes {
		if strings.TrimSpace(value) != "" {
			attributes["term_"+toSnakeCase(key)] = value
		}
	}
	for key, value := range attributes {
		if strings.TrimSpace(value) == "" {
			delete(attributes, key)
		}
	}
	if len(attributes) == 0 {
		return nil
	}
	return attributes
}

func toSnakeCase(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for i, r := range value {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				prev := rune(value[i-1])
				if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
					b.WriteByte('_')
				}
			}
			b.WriteRune(r + ('a' - 'A'))
		} else if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(nonIdentifier.ReplaceAllString(b.String(), "_"), "_")
}
