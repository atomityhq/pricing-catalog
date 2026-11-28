package functions

import (
	"fmt"
	"math/big"
	"pricing-catalog/pkg/catalog"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

func groupPlans(plans []AzurePlan) map[MeterGroupKey][]AzurePlan {
	groups := make(map[MeterGroupKey][]AzurePlan)

	for _, plan := range plans {
		key := MeterGroupKey{
			ProductID:     plan.ProductID,
			SKUID:         plan.SKUID,
			MeterID:       plan.MeterID,
			Region:        plan.ArmRegionName,
			Currency:      plan.CurrencyCode,
			Unit:          plan.UnitOfMeasure,
			Type:          plan.Type,
			EffectiveDate: plan.EffectiveStartDate,
		}

		groups[key] = append(groups[key], plan)
	}

	return groups
}

// sortTiers sorts pricing tiers numerically by their minimum quantity.
func sortTiers(plans []AzurePlan) error {
	thresholds := make(map[string]*big.Rat)

	for _, plan := range plans {
		key := plan.TierMinimumUnits.String()

		threshold, ok := new(big.Rat).SetString(key)
		if !ok || threshold.Sign() < 0 {
			return fmt.Errorf("invalid tier threshold: %q", key)
		}

		thresholds[key] = threshold
	}

	sort.Slice(plans, func(i, j int) bool {
		a := thresholds[plans[i].TierMinimumUnits.String()]
		b := thresholds[plans[j].TierMinimumUnits.String()]

		return a.Cmp(b) < 0
	})

	// Reject duplicate thresholds within the same meter group.
	for i := 1; i < len(plans); i++ {
		a := thresholds[plans[i-1].TierMinimumUnits.String()]
		b := thresholds[plans[i].TierMinimumUnits.String()]

		if a.Cmp(b) == 0 {
			return fmt.Errorf(
				"duplicate tier threshold for meter %q: %s",
				plans[i].MeterID,
				plans[i].TierMinimumUnits.String(),
			)
		}
	}

	return nil
}

// getUnitFactor extracts the numerical quantity from the provider unit.
func getUnitFactor(
	unit string,
	mapping MeterMapping,
) (int64, error) {
	parts := strings.Fields(unit)

	if len(parts) == 0 {
		return 0, fmt.Errorf("empty billing unit")
	}

	factor, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || factor <= 0 {
		return 0, fmt.Errorf("invalid unit factor: %q", unit)
	}

	providerUnit := strings.Join(parts[1:], " ")

	if providerUnit != mapping.ProviderUnit {
		return 0, fmt.Errorf(
			"unexpected billing unit: expected %q, got %q",
			mapping.ProviderUnit,
			providerUnit,
		)
	}

	return factor, nil
}

func normalizePriceAndThreshold(
	plan AzurePlan,
	factor int64,
) (*big.Rat, *big.Rat, error) {
	if factor <= 0 {
		return nil, nil, fmt.Errorf("invalid unit factor")
	}

	price, ok := new(big.Rat).SetString(
		plan.RetailPrice.String(),
	)
	if !ok || price.Sign() < 0 {
		return nil, nil, fmt.Errorf("invalid retail price")
	}

	threshold, ok := new(big.Rat).SetString(
		plan.TierMinimumUnits.String(),
	)
	if !ok || threshold.Sign() < 0 {
		return nil, nil, fmt.Errorf("invalid tier threshold")
	}

	unitFactor := big.NewRat(factor, 1)

	price.Quo(price, unitFactor)
	threshold.Mul(threshold, unitFactor)

	return price, threshold, nil
}

func exactDecimal(value *big.Rat) (string, error) {
	precision, exact := value.FloatPrec()

	if !exact {
		return "", fmt.Errorf(
			"non-terminating decimal: %s",
			value.RatString(),
		)
	}

	return value.FloatString(precision), nil
}

// derive the upper boundary from the next tier.
func getEndQuantity(
	group []AzurePlan,
	index int,
	factor int64,
) (string, error) {
	if index+1 >= len(group) {
		return "", nil
	}

	threshold, ok := new(big.Rat).SetString(
		group[index+1].TierMinimumUnits.String(),
	)
	if !ok || threshold.Sign() < 0 {
		return "", fmt.Errorf("invalid upper tier threshold")
	}

	threshold.Mul(threshold, big.NewRat(factor, 1))

	return exactDecimal(threshold)
}

func parseEffectiveDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}

	date, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid effective date %q: %w", value, err,
		)
	}

	date = date.UTC()

	return &date, nil
}
func normalizeSavingsPlans(
	plan AzurePlan,
	base catalog.PricingRecord,
	factor int64,
) ([]catalog.PricingRecord, error) {

	if factor <= 0 {
		return nil, fmt.Errorf("invalid unit factor")
	}

	records := make(
		[]catalog.PricingRecord, 0, len(plan.SavingsPlan),
	)

	for _, savings := range plan.SavingsPlan {
		term, ok := savingsTermMappings[savings.Term]
		if !ok {
			return nil, fmt.Errorf(
				"unsupported savings plan term: %q",
				savings.Term,
			)
		}

		price, ok := new(big.Rat).SetString(
			savings.RetailPrice.String(),
		)
		if !ok || price.Sign() < 0 {
			return nil, fmt.Errorf("invalid savings plan price")
		}

		price.Quo(price, big.NewRat(factor, 1))

		amount, err := exactDecimal(price)
		if err != nil {
			return nil, err
		}

		record := base

		record.PurchaseModel = "committed"

		record.Dimensions = map[string]string{
			"commitment_term": term,
			"commitment_type": "savings_plan",
		}

		record.Price = catalog.Money{
			Amount:   amount,
			Currency: plan.CurrencyCode,
		}

		// PAYG free grant tiers are not inherited.
		record.Tier = nil

		// Copy attributes to avoid modifying the base record's map.
		record.Attributes = make(map[string]string)

		for key, value := range base.Attributes {
			record.Attributes[key] = value
		}

		delete(record.Attributes, "provider_tier_minimum")

		record.Attributes["provider_retail_price"] =
			savings.RetailPrice.String()

		record.Attributes["provider_unit_price"] =
			savings.UnitPrice.String()

		record.Attributes["provider_savings_term"] =
			savings.Term

		records = append(records, record)
	}

	return records, nil
}
func Normalize(
	plans []AzurePlan,
	source string,
	sourceType string,
) ([]catalog.PricingRecord, error) {

	records := make([]catalog.PricingRecord, 0, len(plans))

	groups := groupPlans(plans)

	savingsRecords := make(map[string]catalog.PricingRecord)

	for _, group := range groups {

		if err := sortTiers(group); err != nil {
			return nil, err
		}

		for i, plan := range group {
			mapping, err := getMeterMapping(plan)
			if err != nil {
				return nil, err
			}

			factor, err := getUnitFactor(
				plan.UnitOfMeasure,
				mapping,
			)
			if err != nil {
				return nil, err
			}

			price, threshold, err := normalizePriceAndThreshold(
				plan,
				factor,
			)
			if err != nil {
				return nil, err
			}

			amount, err := exactDecimal(price)
			if err != nil {
				return nil, err
			}

			startQuantity, err := exactDecimal(threshold)
			if err != nil {
				return nil, err
			}

			endQuantity, err := getEndQuantity(
				group,
				i,
				factor,
			)
			if err != nil {
				return nil, err
			}

			purchaseModel, ok := purchaseModelMappings[plan.Type]
			if !ok {
				return nil, fmt.Errorf(
					"unsupported purchase model: %q",
					plan.Type,
				)
			}

			effectiveFrom, err := parseEffectiveDate(plan.EffectiveStartDate)
			if err != nil {
				return nil, err
			}

			record := catalog.PricingRecord{
				Provider:          "azure",
				Product:           productMappings[plan.ProductName],
				SKU:               skuMappings[plan.SKUName],
				ProviderProductID: plan.ProductID,
				ProviderSKUID:     plan.SKUID,

				Region:           plan.ArmRegionName,
				PurchaseModel:    purchaseModel,
				PricingDimension: mapping.PricingDimension,
				BillingUnit:      mapping.BillingUnit,
				EffectiveFrom:    effectiveFrom,

				Price: catalog.Money{
					Amount:   amount,
					Currency: plan.CurrencyCode,
				},

				Tier: &catalog.Tier{
					StartQuantity: startQuantity,
					EndQuantity:   endQuantity,
				},

				Attributes: map[string]string{
					"meter_id":                plan.MeterID,
					"meter_name":              plan.MeterName,
					"service_id":              plan.ServiceID,
					"service_name":            plan.ServiceName,
					"service_family":          plan.ServiceFamily,
					"arm_sku_name":            plan.ArmSKUName,
					"location":                plan.Location,
					"provider_unit":           plan.UnitOfMeasure,
					"provider_unit_price":     plan.UnitPrice.String(),
					"provider_retail_price":   plan.RetailPrice.String(),
					"provider_tier_minimum":   plan.TierMinimumUnits.String(),
					"is_primary_meter_region": strconv.FormatBool(plan.IsPrimaryMeterRegion),
				},

				Source: catalog.Source{
					Type: sourceType,
					URL:  source,
				},
			}

			committedRecords, err := normalizeSavingsPlans(
				plan, record, factor,
			)
			if err != nil {
				return nil, err
			}

			for _, committed := range committedRecords {
				key := catalog.RecordKey(committed)

				if existing, ok := savingsRecords[key]; ok {
					if !reflect.DeepEqual(existing, committed) {
						return nil, fmt.Errorf(
							"conflicting savings plan records: %s", key,
						)
					}

					continue
				}

				savingsRecords[key] = committed
			}

			records = append(records, record)
		}
	}

	for _, record := range savingsRecords {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return catalog.RecordKey(records[i]) <
			catalog.RecordKey(records[j])
	})

	return records, nil
}
