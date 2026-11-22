package dynamodb

import (
	"testing"

	"pricing-catalog/pkg/catalog"
)

func TestNormalizeRecordsKeepsReservedRecurringAndUpfrontChargesSeparate(t *testing.T) {
	products := map[string]sourceProduct{
		"SKU-RES": {
			SKU:           "SKU-RES",
			ProductFamily: "Provisioned IOPS",
			Attributes: map[string]string{
				"regionCode": "us-east-1",
				"group":      "DDB-ReadUnits",
			},
		},
	}
	terms := []sourceTerm{{
		TermCode:       "TERM-RESERVED",
		OfferTermCode:  "OFFER-RESERVED",
		SKU:            "SKU-RES",
		EffectiveDate:  "2026-01-01T00:00:00Z",
		TermType:       "Reserved",
		TermAttributes: map[string]string{"LeaseContractLength": "1yr", "PurchaseOption": "Partial Upfront"},
		PriceDimensions: []sourcePriceDimension{
			{RateCode: "RATE-HOURLY", Description: "Reserved read capacity", Unit: "ReadCapacityUnit-Hrs", BeginRange: "0", EndRange: "Inf", PricePerUnit: map[string]string{"USD": "0.0001300"}},
			{RateCode: "RATE-UPFRONT", Description: "Partial upfront fee", Unit: "Quantity", BeginRange: "0", EndRange: "Inf", PricePerUnit: map[string]string{"USD": "25.00"}},
		},
	}}

	records, err := normalizeRecords(products, terms)
	if err != nil {
		t.Fatalf("normalizeRecords() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}

	byRate := make(map[string]catalog.PricingRecord, len(records))
	for _, record := range records {
		byRate[record.Attributes["rate_code"]] = record
	}
	hourly := byRate["RATE-HOURLY"]
	if hourly.PurchaseModel != "reserved" || hourly.PricingDimension != "read_capacity" || hourly.BillingUnit != "ReadCapacityUnit-Hrs" {
		t.Errorf("unexpected hourly record: %+v", hourly)
	}
	upfront := byRate["RATE-UPFRONT"]
	if upfront.PricingDimension != "upfront_fee" || upfront.BillingUnit != "Quantity" || upfront.Price.Amount != "25.00" {
		t.Errorf("unexpected upfront record: %+v", upfront)
	}
	if upfront.Attributes["term_purchase_option"] != "Partial Upfront" {
		t.Errorf("purchase option was not preserved: %+v", upfront.Attributes)
	}
}

func TestNormalizeRegionHandlesGlobalAndTestProducts(t *testing.T) {
	global, include, err := normalizeRegion(sourceProduct{Attributes: map[string]string{
		"location":  "Any",
		"usagetype": "Global-VectorSearch",
	}})
	if err != nil || !include || global != "global" {
		t.Fatalf("global region = %q, include=%v, err=%v", global, include, err)
	}

	_, include, err = normalizeRegion(sourceProduct{Attributes: map[string]string{
		"location":  "TST",
		"usagetype": "TST1-VectorSearch",
	}})
	if err != nil || include {
		t.Fatalf("test region should be excluded, include=%v, err=%v", include, err)
	}
}

func TestBuildTierPreservesFiniteAndOpenEndedRanges(t *testing.T) {
	finite, err := buildTier(sourcePriceDimension{BeginRange: "0", EndRange: "18600"})
	if err != nil || finite == nil || finite.StartQuantity != "0" || finite.EndQuantity != "18600" {
		t.Fatalf("finite tier = %+v, err=%v", finite, err)
	}

	open, err := buildTier(sourcePriceDimension{BeginRange: "18600", EndRange: "Inf"})
	if err != nil || open == nil || open.StartQuantity != "18600" || open.EndQuantity != "" {
		t.Fatalf("open tier = %+v, err=%v", open, err)
	}
}

func TestClassifyPricingDimensionIncludesCDCWithoutProductFamily(t *testing.T) {
	product := sourceProduct{Attributes: map[string]string{"group": "DDB-Kinesis"}}
	got := classifyPricingDimension(product, sourceTerm{}, sourcePriceDimension{Unit: "ChangeDataCaptureUnits"})
	if got != "change_data_capture" {
		t.Fatalf("classification = %q, want change_data_capture", got)
	}
}

func TestClassifyPricingDimensionDistinguishesStreamsFromOrdinaryAPIRequests(t *testing.T) {
	tests := []struct {
		name    string
		product sourceProduct
		want    string
	}{
		{
			name: "ordinary read request",
			product: sourceProduct{
				ProductFamily: "API Request",
				Attributes: map[string]string{
					"group":     "DDB-ReadUnits",
					"usagetype": "ReadRequestUnits",
				},
			},
			want: "read_request",
		},
		{
			name: "streams read request",
			product: sourceProduct{
				ProductFamily: "API Request",
				Attributes: map[string]string{
					"group": "DDB-StreamsReadRequests",
				},
			},
			want: "streams_read_request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyPricingDimension(
				tt.product,
				sourceTerm{},
				sourcePriceDimension{Unit: "Requests"},
			)

			if got != tt.want {
				t.Errorf("classification = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyPricingDimensionDoesNotTreatOnDemandQuantityAsUpfront(t *testing.T) {
	product := sourceProduct{
		Attributes: map[string]string{
			"group": "DDB-Kinesis",
		},
	}

	term := sourceTerm{
		TermType: "OnDemand",
	}

	dim := sourcePriceDimension{
		Unit:        "Quantity",
		Description: "Change data capture quantity",
	}

	got := classifyPricingDimension(product, term, dim)

	if got != "change_data_capture" {
		t.Errorf("classification = %q, want change_data_capture", got)
	}
}

func TestClassifyPricingDimensionRecognizesReservedUpfrontQuantity(t *testing.T) {
	product := sourceProduct{
		Attributes: map[string]string{
			"group": "DDB-ReadUnits",
		},
	}

	term := sourceTerm{
		TermType: "Reserved",
	}

	dim := sourcePriceDimension{
		Unit:        "Quantity",
		Description: "Reserved read capacity",
	}

	got := classifyPricingDimension(product, term, dim)

	if got != "upfront_fee" {
		t.Errorf("classification = %q, want upfront_fee", got)
	}
}

func TestClassifyPricingDimensionRecognizesReservedCaseInsensitively(t *testing.T) {
	product := sourceProduct{
		Attributes: map[string]string{"group": "DDB-ReadUnits"},
	}

	term := sourceTerm{TermType: "reserved"}
	dim := sourcePriceDimension{Unit: "Quantity"}

	got := classifyPricingDimension(product, term, dim)

	if got != "upfront_fee" {
		t.Errorf("classification = %q, want upfront_fee", got)
	}
}
