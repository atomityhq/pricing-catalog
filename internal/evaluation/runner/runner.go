package runner

import (
	"context"
	"fmt"
	"pricing-catalog/internal/evaluation/cases"
	"pricing-catalog/internal/pipeline"
	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
	"reflect"
	"time"
)

type Options struct {
	Version     string
	GeneratedAt time.Time
}

type Result struct {
	CaseID string
	Passed bool
	Error  error
}

func Run(ctx context.Context, c connector.Connector, suite *cases.Suite, options pipeline.BuildOptions) ([]Result, error) {
	if suite == nil {
		return nil, fmt.Errorf("evaluation suite is nil")
	}

	snapshot, buildErr := pipeline.BuildSnapshot(ctx, c, options)
	results := make([]Result, 0, len(suite.Cases))

	for _, testCase := range suite.Cases {
		result := Result{CaseID: testCase.ID}
		if testCase.Expect.Error {
			if buildErr == nil {
				result.Error = fmt.Errorf("expected connector/pipeline error, but build succeeded")
			} else {
				result.Passed = true
			}
			results = append(results, result)
			continue
		}

		if buildErr != nil {
			result.Error = fmt.Errorf("build candidate catalog: %w", buildErr)
			results = append(results, result)
			continue
		}

		if testCase.Expect.Deterministic {
			if err := checkDeterministic(ctx, c, options, snapshot); err != nil {
				result.Error = err
				results = append(results, result)
				continue
			}
		}

		if err := checkRecords(snapshot.Records, testCase.Expect.Records); err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}

		result.Passed = true
		results = append(results, result)

	}

	return results, nil
}

func checkDeterministic(
	ctx context.Context,
	c connector.Connector,
	options pipeline.BuildOptions,
	first catalog.Snapshot,
) error {
	second, err := pipeline.BuildSnapshot(ctx, c, options)
	if err != nil {
		return fmt.Errorf("second deterministic build failed: %w", err)
	}

	if !reflect.DeepEqual(first, second) {
		return fmt.Errorf("connector produced different catalog output across identical builds")
	}

	return nil
}

func checkRecords(records []catalog.PricingRecord, expectations []*cases.RecordExpectation) error {
	for i, expectation := range expectations {
		matches := matchingRecords(records, expectation.Match, expectation.Values)
		if len(matches) == 0 {
			return fmt.Errorf("record expectation %d: no matching record", i)
		}

		if len(matches) > 1 {
			return fmt.Errorf(
				"record expectation %d: match identifies %d records; expected exactly one",
				i,
				len(matches),
			)
		}

		if err := assertRecord(matches[0], expectation); err != nil {
			return fmt.Errorf("record expectation %d: %w", i, err)
		}
	}

	return nil
}

func matchingRecords(
	records []catalog.PricingRecord,
	match *cases.FieldMatchers,
	values *cases.RecordValues,
) []catalog.PricingRecord {
	if match == nil {
		return nil
	}

	var result []catalog.PricingRecord
	for _, record := range records {
		if match.Provider == "exact" && (values == nil || record.Provider != values.Provider) {
			continue
		}
		if match.Product == "exact" && (values == nil || record.Product != values.Product) {
			continue
		}
		if match.SKU == "exact" && (values == nil || record.SKU != values.SKU) {
			continue
		}
		if match.ProviderProductID == "exact" && (values == nil || record.ProviderProductID != values.ProviderProductID) {
			continue
		}
		if match.ProviderSKUID == "exact" && (values == nil || record.ProviderSKUID != values.ProviderSKUID) {
			continue
		}
		if match.Region == "exact" && (values == nil || record.Region != values.Region) {
			continue
		}
		if match.PurchaseModel == "exact" && (values == nil || record.PurchaseModel != values.PurchaseModel) {
			continue
		}
		if match.BillingUnit == "exact" && (values == nil || record.BillingUnit != values.BillingUnit) {
			continue
		}
		if match.PricingDimension == "exact" && (values == nil || record.PricingDimension != values.PricingDimension) {
			continue
		}

		result = append(result, record)
	}
	return result
}

func assertRecord(
	record catalog.PricingRecord,
	expectation *cases.RecordExpectation,
) error {
	if expectation.Values != nil {
		v := expectation.Values

		if v.Provider != "" && record.Provider != v.Provider {
			return fmt.Errorf("provider: got %q, want %q", record.Provider, v.Provider)
		}
		if v.Product != "" && record.Product != v.Product {
			return fmt.Errorf("product: got %q, want %q", record.Product, v.Product)
		}
		if v.SKU != "" && record.SKU != v.SKU {
			return fmt.Errorf("sku: got %q, want %q", record.SKU, v.SKU)
		}
		if v.ProviderProductID != "" && record.ProviderProductID != v.ProviderProductID {
			return fmt.Errorf("provider_product_id: got %q, want %q", record.ProviderProductID, v.ProviderProductID)
		}
		if v.ProviderSKUID != "" && record.ProviderSKUID != v.ProviderSKUID {
			return fmt.Errorf("provider_sku_id: got %q, want %q", record.ProviderSKUID, v.ProviderSKUID)
		}
		if v.Region != "" && record.Region != v.Region {
			return fmt.Errorf("region: got %q, want %q", record.Region, v.Region)
		}
		if v.PurchaseModel != "" && record.PurchaseModel != v.PurchaseModel {
			return fmt.Errorf("purchase_model: got %q, want %q", record.PurchaseModel, v.PurchaseModel)
		}
		if v.BillingUnit != "" && record.BillingUnit != v.BillingUnit {
			return fmt.Errorf("billing_unit: got %q, want %q", record.BillingUnit, v.BillingUnit)
		}
		if v.PricingDimension != "" && record.PricingDimension != v.PricingDimension {
			return fmt.Errorf("pricing_dimension: got %q, want %q", record.PricingDimension, v.PricingDimension)
		}
		if v.Dimensions != nil && !reflect.DeepEqual(record.Dimensions, v.Dimensions) {
			return fmt.Errorf("dimensions: got %#v, want %#v", record.Dimensions, v.Dimensions)
		}
		if v.Attributes != nil && !reflect.DeepEqual(record.Attributes, v.Attributes) {
			return fmt.Errorf("attributes: got %#v, want %#v", record.Attributes, v.Attributes)
		}
		if v.Tier != nil {
			if record.Tier == nil {
				return fmt.Errorf("tier: got nil, want %#v", v.Tier)
			}
			if record.Tier.StartQuantity != v.Tier.StartQuantity {
				return fmt.Errorf("tier.start_quantity: got %q, want %q", record.Tier.StartQuantity, v.Tier.StartQuantity)
			}
			if record.Tier.EndQuantity != v.Tier.EndQuantity {
				return fmt.Errorf("tier.end_quantity: got %q, want %q", record.Tier.EndQuantity, v.Tier.EndQuantity)
			}
		}

		effectiveFrom, err := cases.ParseTime(v.EffectiveFrom)
		if err != nil {
			return fmt.Errorf("effective_from: %w", err)
		}
		if effectiveFrom != nil && !sameTime(record.EffectiveFrom, effectiveFrom) {
			return fmt.Errorf("effective_from: got %v, want %v", record.EffectiveFrom, effectiveFrom)
		}

		effectiveTo, err := cases.ParseTime(v.EffectiveTo)
		if err != nil {
			return fmt.Errorf("effective_to: %w", err)
		}
		if effectiveTo != nil && !sameTime(record.EffectiveTo, effectiveTo) {
			return fmt.Errorf("effective_to: got %v, want %v", record.EffectiveTo, effectiveTo)
		}

		if v.Source != nil {
			if record.Source.Type != v.Source.Type {
				return fmt.Errorf("source.type: got %q, want %q", record.Source.Type, v.Source.Type)
			}
			if record.Source.URL != v.Source.URL {
				return fmt.Errorf("source.url: got %q, want %q", record.Source.URL, v.Source.URL)
			}
		}
	}

	if expectation.Price != nil {
		if record.Price.Amount != expectation.Price.Amount {
			return fmt.Errorf(
				"price.amount: got %q, want %q",
				record.Price.Amount,
				expectation.Price.Amount,
			)
		}
		if record.Price.Currency != expectation.Price.Currency {
			return fmt.Errorf(
				"price.currency: got %q, want %q",
				record.Price.Currency,
				expectation.Price.Currency,
			)
		}
	}

	return nil
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
