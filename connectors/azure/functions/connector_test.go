package functions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pricing-catalog/internal/pipeline"
	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
	"pricing-catalog/pkg/connector/testkit"
)

// ---------- Fixture helpers ----------

// item builds one Azure retail price row. Defaults describe the France Central
// Flex Consumption "On Demand Total Executions" meter. Overrides replace keys.
func item(over map[string]any) map[string]any {
	row := map[string]any{
		"currencyCode":         "USD",
		"tierMinimumUnits":     json.Number("0"),
		"retailPrice":          json.Number("0.000004"),
		"unitPrice":            json.Number("0.000004"),
		"armRegionName":        "francecentral",
		"location":             "FR Central",
		"effectiveStartDate":   "2025-06-01T00:00:00Z",
		"meterId":              "04bf9ffe-7320-5321-9ed7-661107c8008a",
		"meterName":            "On Demand Total Executions",
		"productId":            "DZH318Z0DW2R",
		"skuId":                "DZH318Z0DW2R/0095",
		"productName":          "Flex Consumption",
		"skuName":              "On Demand",
		"serviceName":          "Functions",
		"serviceId":            "DZH319HJX2WX",
		"serviceFamily":        "Compute",
		"unitOfMeasure":        "10",
		"type":                 "Consumption",
		"isPrimaryMeterRegion": true,
		"armSkuName":           "On Demand",
		"savingsPlan":          []any{},
	}
	for k, v := range over {
		row[k] = v
	}
	return row
}

func num(s string) json.Number { return json.Number(s) }

func saving(term, retail, unit string) map[string]any {
	return map[string]any{
		"term":        term,
		"retailPrice": json.Number(retail),
		"unitPrice":   json.Number(unit),
	}
}

func payload(t *testing.T, items ...map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"Items":        items,
		"NextPageLink": "",
		"Count":        len(items),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return data
}

func decodePlans(t *testing.T, data []byte) []AzurePlan {
	t.Helper()
	var response AzureResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("decode plans: %v", err)
	}
	return response.Items
}

func normalize(t *testing.T, items ...map[string]any) ([]catalog.PricingRecord, error) {
	t.Helper()
	return Normalize(decodePlans(t, payload(t, items...)), "test://source", SourceType)
}

func mustNormalize(t *testing.T, items ...map[string]any) []catalog.PricingRecord {
	t.Helper()
	records, err := normalize(t, items...)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return records
}

func where(records []catalog.PricingRecord, keep func(catalog.PricingRecord) bool) []catalog.PricingRecord {
	var out []catalog.PricingRecord
	for _, r := range records {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// bothSavings is the pair of terms Azure returns on most rows.
func bothSavings(retail string) []any {
	return []any{
		saving("3 Years", retail, "0"),
		saving("1 Year", retail, "0"),
	}
}

func loadFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "source.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func fixtureItems(t *testing.T) []map[string]any {
	t.Helper()
	var data struct{ Items []map[string]any }
	decoder := json.NewDecoder(bytes.NewReader(loadFixture(t)))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return data.Items
}

func fixtureRecords(t *testing.T) []catalog.PricingRecord {
	t.Helper()
	return mustNormalize(t, fixtureItems(t)...)
}

func newFixtureConnector(t *testing.T) *Connector {
	t.Helper()
	data := loadFixture(t)
	c := New()
	c.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != SourceURL || r.Method != http.MethodGet || r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected request: %s %s, Accept=%q", r.Method, r.URL, r.Header.Get("Accept"))
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	})}
	return c
}

// ---------- Contract and pipeline ----------

func TestConnectorContract(t *testing.T) {
	records := testkit.AssertConnectorContract(t, func() connector.Connector {
		return newFixtureConnector(t)
	})
	if len(records) != 25 {
		t.Fatalf("got %d records, want 25", len(records))
	}
}

func TestPipelineBuildSnapshot(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)

	snapshot, err := pipeline.BuildSnapshot(
		context.Background(),
		newFixtureConnector(t),
		pipeline.BuildOptions{Version: "v1.0.0", GeneratedAt: now},
	)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(snapshot.Records) != 25 {
		t.Fatalf("got %d records, want 25", len(snapshot.Records))
	}
	if err := catalog.ValidateRecords(snapshot.Records); err != nil {
		t.Fatalf("ValidateRecords: %v", err)
	}
}

func TestAllRecordsValidate(t *testing.T) {
	for i, r := range fixtureRecords(t) {
		if err := catalog.ValidateRecord(r); err != nil {
			t.Errorf("record %d (%s %s): %v", i, r.Region, r.PricingDimension, err)
		}
	}
}

func TestNoRecordKeyCollisions(t *testing.T) {
	seen := make(map[string]int)
	for i, r := range fixtureRecords(t) {
		key := catalog.RecordKey(r)
		if prev, ok := seen[key]; ok {
			t.Errorf("record %d collides with record %d: %s", i, prev, key)
		}
		seen[key] = i
	}
}

func TestDeterministicOutput(t *testing.T) {
	first := fixtureRecords(t)
	for i := 0; i < 20; i++ {
		got := fixtureRecords(t)
		if !reflect.DeepEqual(first, got) {
			t.Fatalf("run %d produced different output", i)
		}
	}
}

func TestOutputIsSortedByRecordKey(t *testing.T) {
	records := fixtureRecords(t)
	for i := 1; i < len(records); i++ {
		if catalog.RecordKey(records[i-1]) > catalog.RecordKey(records[i]) {
			t.Fatalf("records %d and %d are out of order", i-1, i)
		}
	}
}

// ---------- Tiers, free grants and unit conversion ----------

func TestFlexExecutionsFreeGrantTiers(t *testing.T) {
	records := where(fixtureRecords(t), func(r catalog.PricingRecord) bool {
		return r.Region == "francecentral" &&
			r.PurchaseModel == "on_demand" &&
			r.PricingDimension == "executions"
	})
	if len(records) != 2 {
		t.Fatalf("got %d tier records, want 2", len(records))
	}

	byStart := map[string]catalog.PricingRecord{}
	for _, r := range records {
		if r.Tier == nil {
			t.Fatal("tier must be set")
		}
		byStart[r.Tier.StartQuantity] = r
	}

	// Unit "10" means the price is per 10 executions. Thresholds scale by 10
	// and prices divide by 10, so the canonical unit is a single execution.
	free, ok := byStart["0"]
	if !ok {
		t.Fatal("missing free grant tier starting at 0")
	}
	if free.Tier.EndQuantity != "250000" {
		t.Errorf("free tier end: got %q, want 250000", free.Tier.EndQuantity)
	}
	if free.Price.Amount != "0" {
		t.Errorf("free tier amount: got %q, want 0", free.Price.Amount)
	}

	paid, ok := byStart["250000"]
	if !ok {
		t.Fatal("missing paid tier starting at 250000")
	}
	if paid.Tier.EndQuantity != "" {
		t.Errorf("last tier must be open ended, got %q", paid.Tier.EndQuantity)
	}
	if paid.Price.Amount != "0.0000004" {
		t.Errorf("paid tier amount: got %q, want 0.0000004", paid.Price.Amount)
	}
	for _, r := range records {
		if r.BillingUnit != "execution" {
			t.Errorf("BillingUnit: got %q, want execution", r.BillingUnit)
		}
	}
}

func TestExecutionTimeTiersWithUnitFactorOne(t *testing.T) {
	records := where(fixtureRecords(t), func(r catalog.PricingRecord) bool {
		return r.Region == "centralindia" &&
			r.PurchaseModel == "on_demand" &&
			r.PricingDimension == "execution_duration"
	})
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	for _, r := range records {
		if r.BillingUnit != "GB-second" {
			t.Errorf("BillingUnit: got %q, want GB-second", r.BillingUnit)
		}
		switch r.Tier.StartQuantity {
		case "0":
			if r.Tier.EndQuantity != "100000" || r.Price.Amount != "0" {
				t.Errorf("free tier: got end %q amount %q", r.Tier.EndQuantity, r.Price.Amount)
			}
		case "100000":
			if r.Tier.EndQuantity != "" || r.Price.Amount != "0.000037" {
				t.Errorf("paid tier: got end %q amount %q", r.Tier.EndQuantity, r.Price.Amount)
			}
		default:
			t.Errorf("unexpected tier start %q", r.Tier.StartQuantity)
		}
	}
}

func TestSingleTierMeter(t *testing.T) {
	records := where(fixtureRecords(t), func(r catalog.PricingRecord) bool {
		return r.Region == "southcentralus" && r.PurchaseModel == "on_demand"
	})
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	r := records[0]
	if r.Tier == nil || r.Tier.StartQuantity != "0" || r.Tier.EndQuantity != "" {
		t.Errorf("tier: got %+v, want 0 to open end", r.Tier)
	}
	if r.PricingDimension != "baseline_duration" || r.Price.Amount != "0.000004" {
		t.Errorf("got dimension %q amount %q", r.PricingDimension, r.Price.Amount)
	}
	if r.Product != "flex-consumption" || r.SKU != "always-ready" {
		t.Errorf("got product %q sku %q", r.Product, r.SKU)
	}
}

func TestPremiumMeterMapping(t *testing.T) {
	records := where(fixtureRecords(t), func(r catalog.PricingRecord) bool {
		return r.Region == "australiaeast" && r.PurchaseModel == "on_demand"
	})
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	r := records[0]
	if r.Product != "premium" || r.SKU != "premium" {
		t.Errorf("got product %q sku %q", r.Product, r.SKU)
	}
	if r.PricingDimension != "memory_duration" || r.BillingUnit != "GiB-hour" {
		t.Errorf("got dimension %q unit %q", r.PricingDimension, r.BillingUnit)
	}
	if r.Price.Amount != "0.013" {
		t.Errorf("amount: got %q, want 0.013", r.Price.Amount)
	}
}

func TestPremiumVCPUMeterMapping(t *testing.T) {
	records := mustNormalize(t, item(map[string]any{
		"armRegionName": "westus3",
		"meterName":     "Premium vCPU Duration",
		"productName":   "Premium Functions",
		"skuName":       "Premium",
		"unitOfMeasure": "1 Hour",
		"retailPrice":   num("0.173"),
		"unitPrice":     num("0.173"),
	}))
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if records[0].PricingDimension != "vcpu_duration" || records[0].BillingUnit != "vCPU-hour" {
		t.Errorf("got %q %q", records[0].PricingDimension, records[0].BillingUnit)
	}
}

func TestUnsortedTiersProduceSameOutput(t *testing.T) {
	items := fixtureItems(t)
	sorted := mustNormalize(t, items...)

	reversed := make([]map[string]any, len(items))
	for i := range items {
		reversed[len(items)-1-i] = items[i]
	}
	got := mustNormalize(t, reversed...)

	if !reflect.DeepEqual(sorted, got) {
		t.Fatal("input order must not change normalized output")
	}
}

func TestTierThresholdsComparedNumerically(t *testing.T) {
	// A string sort would place "100" before "9".
	base := map[string]any{
		"armRegionName": "westus3",
		"meterName":     "Always Ready Baseline",
		"skuName":       "Always Ready",
		"armSkuName":    "Always Ready",
		"unitOfMeasure": "1 GB Second",
	}
	with := func(tier, price string) map[string]any {
		row := map[string]any{"tierMinimumUnits": num(tier), "retailPrice": num(price), "unitPrice": num(price)}
		for k, v := range base {
			row[k] = v
		}
		return item(row)
	}

	records := mustNormalize(t, with("100", "0.3"), with("9", "0.2"), with("0", "0.1"))
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3", len(records))
	}

	want := map[string][2]string{
		"0":   {"9", "0.1"},
		"9":   {"100", "0.2"},
		"100": {"", "0.3"},
	}
	for _, r := range records {
		w, ok := want[r.Tier.StartQuantity]
		if !ok {
			t.Fatalf("unexpected tier start %q", r.Tier.StartQuantity)
		}
		if r.Tier.EndQuantity != w[0] || r.Price.Amount != w[1] {
			t.Errorf("tier %s: got end %q amount %q, want end %q amount %q",
				r.Tier.StartQuantity, r.Tier.EndQuantity, r.Price.Amount, w[0], w[1])
		}
	}
}

// ---------- Savings plans ----------

func TestSavingsPlansBecomeCommittedRecords(t *testing.T) {
	committed := where(fixtureRecords(t), func(r catalog.PricingRecord) bool {
		return r.PurchaseModel == "committed"
	})
	if len(committed) != 12 {
		t.Fatalf("got %d committed records, want 12", len(committed))
	}

	for _, r := range committed {
		if r.Tier != nil {
			t.Errorf("savings plan records must not inherit the free grant tier, got %+v", r.Tier)
		}
		if r.Dimensions["commitment_type"] != "savings_plan" {
			t.Errorf("commitment_type: got %q", r.Dimensions["commitment_type"])
		}
		term := r.Dimensions["commitment_term"]
		if term != "1_year" && term != "3_year" {
			t.Errorf("commitment_term: got %q", term)
		}
		if _, ok := r.Attributes["provider_tier_minimum"]; ok {
			t.Error("provider_tier_minimum must not be copied to savings plan records")
		}
	}
}

func TestSavingsPlanDeduplicatedAcrossTiers(t *testing.T) {
	// France Central executions has two tier rows that repeat the same savings plans.
	committed := where(fixtureRecords(t), func(r catalog.PricingRecord) bool {
		return r.Region == "francecentral" && r.PurchaseModel == "committed"
	})
	if len(committed) != 2 {
		t.Fatalf("got %d committed records, want 2 (one per term)", len(committed))
	}
	for _, r := range committed {
		// 0.00000332 per 10 executions is 0.000000332 per execution.
		if r.Price.Amount != "0.000000332" {
			t.Errorf("amount: got %q, want 0.000000332", r.Price.Amount)
		}
		if r.BillingUnit != "execution" {
			t.Errorf("BillingUnit: got %q", r.BillingUnit)
		}
	}
}

func TestSavingsPlanKeepsProviderPrices(t *testing.T) {
	// Azure reports unitPrice 0 on some savings rows. The raw values must survive.
	committed := where(fixtureRecords(t), func(r catalog.PricingRecord) bool {
		return r.Region == "centralindia" &&
			r.PurchaseModel == "committed" &&
			r.Dimensions["commitment_term"] == "1_year"
	})
	if len(committed) != 1 {
		t.Fatalf("got %d records, want 1", len(committed))
	}
	attrs := committed[0].Attributes
	if attrs["provider_savings_term"] != "1 Year" {
		t.Errorf("provider_savings_term: got %q", attrs["provider_savings_term"])
	}
	if attrs["provider_retail_price"] != "0.00003071" {
		t.Errorf("provider_retail_price: got %q", attrs["provider_retail_price"])
	}
	if attrs["provider_unit_price"] != "0.0" {
		t.Errorf("provider_unit_price: got %q", attrs["provider_unit_price"])
	}
	if committed[0].Price.Amount != "0.00003071" {
		t.Errorf("amount: got %q", committed[0].Price.Amount)
	}
}

func TestSavingsPlanDoesNotMutateBaseAttributes(t *testing.T) {
	records := where(fixtureRecords(t), func(r catalog.PricingRecord) bool {
		return r.PurchaseModel == "on_demand"
	})
	if len(records) != 13 {
		t.Fatalf("got %d base records, want 13", len(records))
	}
	for _, r := range records {
		if _, ok := r.Attributes["provider_tier_minimum"]; !ok {
			t.Errorf("base record lost provider_tier_minimum (%s %s)", r.Region, r.PricingDimension)
		}
		if _, ok := r.Attributes["provider_savings_term"]; ok {
			t.Errorf("base record picked up savings attributes (%s)", r.Region)
		}
	}
}

func TestNoSavingsPlanProducesOnlyBaseRecords(t *testing.T) {
	records := mustNormalize(t, item(nil))
	if len(records) != 1 || records[0].PurchaseModel != "on_demand" {
		t.Fatalf("got %d records, want 1 on demand", len(records))
	}
}

func TestConflictingSavingsPlansAcrossTiersError(t *testing.T) {
	_, err := normalize(t,
		item(map[string]any{"savingsPlan": bothSavings("0.00000332")}),
		item(map[string]any{
			"tierMinimumUnits": num("25000"),
			"savingsPlan":      bothSavings("0.00000999"),
		}),
	)
	if err == nil || !strings.Contains(err.Error(), "conflicting savings plan") {
		t.Fatalf("got %v, want conflicting savings plan error", err)
	}
}

// ---------- Identifiers, regions and metadata ----------

func TestMultipleRegionsPreserved(t *testing.T) {
	regions := map[string]bool{}
	for _, r := range fixtureRecords(t) {
		regions[r.Region] = true
	}
	for _, want := range []string{"francecentral", "centralindia", "southcentralus", "australiaeast"} {
		if !regions[want] {
			t.Errorf("missing region %q", want)
		}
	}
}

func TestSameMeterInTwoRegionsStaysSeparate(t *testing.T) {
	records := mustNormalize(t,
		item(map[string]any{"armRegionName": "francecentral", "location": "FR Central"}),
		item(map[string]any{
			"armRegionName": "germanywestcentral",
			"location":      "DE West Central",
			"retailPrice":   num("0.000005"),
			"unitPrice":     num("0.000005"),
		}),
	)
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if records[0].Price.Amount == records[1].Price.Amount {
		t.Error("regional prices must not be merged")
	}
}

func TestProviderIdentifiersAndMetadata(t *testing.T) {
	records := where(fixtureRecords(t), func(r catalog.PricingRecord) bool {
		return r.Region == "southcentralus" && r.PurchaseModel == "on_demand"
	})
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	r := records[0]

	if r.Provider != "azure" {
		t.Errorf("Provider: got %q", r.Provider)
	}
	if r.ProviderProductID != "DZH318Z0DW2R" || r.ProviderSKUID != "DZH318Z0DW2R/0076" {
		t.Errorf("identifiers: got %q, %q", r.ProviderProductID, r.ProviderSKUID)
	}
	if r.Price.Currency != "USD" {
		t.Errorf("Currency: got %q", r.Price.Currency)
	}
	if r.Source.Type != SourceType || r.Source.URL != "test://source" {
		t.Errorf("Source: got %+v", r.Source)
	}
	if r.EffectiveFrom == nil || !r.EffectiveFrom.Equal(time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("EffectiveFrom: got %v", r.EffectiveFrom)
	}

	wantAttrs := map[string]string{
		"meter_id":                "051a85df-ba8b-53d1-ba90-a11812cdd670",
		"meter_name":              "Always Ready Baseline",
		"service_id":              "DZH319HJX2WX",
		"service_name":            "Functions",
		"service_family":          "Compute",
		"arm_sku_name":            "Always Ready",
		"location":                "US South Central",
		"provider_unit":           "1 GB Second",
		"provider_unit_price":     "0.000004",
		"provider_retail_price":   "0.000004",
		"provider_tier_minimum":   "0.0",
		"is_primary_meter_region": "true",
	}
	for key, want := range wantAttrs {
		if got := r.Attributes[key]; got != want {
			t.Errorf("Attributes[%q]: got %q, want %q", key, got, want)
		}
	}
}

func TestEmptyEffectiveDateLeavesEffectiveFromNil(t *testing.T) {
	records := mustNormalize(t, item(map[string]any{"effectiveStartDate": ""}))
	if records[0].EffectiveFrom != nil {
		t.Errorf("EffectiveFrom: got %v, want nil", records[0].EffectiveFrom)
	}
}

func TestNonUTCEffectiveDateIsConvertedToUTC(t *testing.T) {
	records := mustNormalize(t, item(map[string]any{"effectiveStartDate": "2025-06-01T05:00:00+05:00"}))
	got := records[0].EffectiveFrom
	if got == nil || got.Location() != time.UTC || !got.Equal(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("EffectiveFrom: got %v", got)
	}
}

func TestPurchaseModelValues(t *testing.T) {
	// Pins current values so a casing change is a deliberate decision.
	models := map[string]bool{}
	for _, r := range fixtureRecords(t) {
		models[r.PurchaseModel] = true
	}
	if len(models) != 2 || !models["on_demand"] || !models["committed"] {
		t.Errorf("purchase models: got %v", models)
	}
}

func TestExactDecimalsAreNotRoundedThroughFloats(t *testing.T) {
	records := mustNormalize(t, item(map[string]any{
		"unitOfMeasure": "1 GB Second",
		"meterName":     "On Demand Execution Time",
		"retailPrice":   num("0.000016000000001"),
		"unitPrice":     num("0.000016000000001"),
	}))
	if records[0].Price.Amount != "0.000016000000001" {
		t.Errorf("amount: got %q", records[0].Price.Amount)
	}
}

// ---------- Error handling ----------

func TestNormalizeErrors(t *testing.T) {
	flex := func(over map[string]any) map[string]any { return item(over) }

	for _, tc := range []struct {
		name string
		row  map[string]any
		want string
	}{
		{"unknown meter", flex(map[string]any{"meterName": "Mystery Meter"}), "unsupported pricing combination"},
		{"unknown sku", flex(map[string]any{"skuName": "Mystery SKU"}), "unsupported pricing combination"},
		{"unexpected unit text", flex(map[string]any{
			"meterName": "Always Ready Baseline", "skuName": "Always Ready", "unitOfMeasure": "1 Hour",
		}), "unexpected billing unit"},
		{"unit without number", flex(map[string]any{
			"meterName": "Always Ready Baseline", "skuName": "Always Ready", "unitOfMeasure": "GB Second",
		}), "invalid unit factor"},
		{"zero unit factor", flex(map[string]any{"unitOfMeasure": "0"}), "invalid unit factor"},
		{"empty unit", flex(map[string]any{"unitOfMeasure": ""}), "empty billing unit"},
		{"whitespace unit", flex(map[string]any{"unitOfMeasure": "   "}), "empty billing unit"},
		{"unsupported type", flex(map[string]any{"type": "DevTestConsumption"}), "unsupported purchase model"},
		{"reservation type", flex(map[string]any{"type": "Reservation"}), "unsupported purchase model"},
		{"bad effective date", flex(map[string]any{"effectiveStartDate": "yesterday"}), "invalid effective date"},
		{"negative price", flex(map[string]any{"retailPrice": num("-1")}), "invalid retail price"},
		{"negative tier", flex(map[string]any{"tierMinimumUnits": num("-1")}), "invalid tier threshold"},
		{"unsupported savings term", flex(map[string]any{
			"savingsPlan": []any{saving("5 Years", "0.1", "0.1")},
		}), "unsupported savings plan term"},
		{"negative savings price", flex(map[string]any{
			"savingsPlan": []any{saving("1 Year", "-0.1", "0.1")},
		}), "invalid savings plan price"},
		{"non terminating price", flex(map[string]any{
			"unitOfMeasure": "3", "retailPrice": num("1"), "unitPrice": num("1"),
		}), "non-terminating decimal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records, err := normalize(t, tc.row)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got error %v, want it to contain %q", err, tc.want)
			}
			if records != nil {
				t.Errorf("expected nil records on error, got %d", len(records))
			}
		})
	}
}

func TestDuplicateTierThresholdError(t *testing.T) {
	_, err := normalize(t, item(nil), item(map[string]any{"retailPrice": num("0.000009")}))
	if err == nil || !strings.Contains(err.Error(), "duplicate tier threshold") {
		t.Fatalf("got %v, want duplicate tier threshold error", err)
	}
}

func TestMalformedJSONError(t *testing.T) {
	c := New()
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"Items": [`))}, nil
	})
	records, err := c.Fetch(context.Background())
	if err == nil || records != nil {
		t.Fatalf("got %v, %v; want malformed JSON error without records", records, err)
	}
}

func TestNormalizeEmptyPlans(t *testing.T) {
	for _, plans := range [][]AzurePlan{nil, {}} {
		records, err := Normalize(plans, "test://source", SourceType)
		if err != nil || len(records) != 0 {
			t.Fatalf("got %v, %v; want no records and no error", records, err)
		}
	}
}

func TestFetchEmptyItemsError(t *testing.T) {
	c := New()
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"Items":[],"NextPageLink":null,"Count":0}`))}, nil
	})
	records, err := c.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "empty Azure pricing response") || records != nil {
		t.Fatalf("got %v, %v; want empty pricing response error", records, err)
	}
}

// ---------- HTTP ----------

func newHTTPConnector(url string, client *http.Client) *Connector {
	return &Connector{httpClient: client, sourceURL: url, sourceType: SourceType}
}

func TestNewDefaults(t *testing.T) {
	c := New()
	if c.Name() != Name || c.sourceURL != SourceURL || c.sourceType != SourceType || c.httpClient == nil || c.httpClient.Timeout != Timeout {
		t.Fatalf("invalid defaults: %#v", c)
	}
	if !strings.Contains(SourceURL, "prices.azure.com") || !strings.Contains(SourceURL, "Functions") {
		t.Errorf("unexpected SourceURL %q", SourceURL)
	}
}

func TestFetchHTTP(t *testing.T) {
	data := payload(t, fixtureItems(t)...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/json" {
			t.Errorf("request: %s, Accept=%q", r.Method, r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer server.Close()

	records, err := newHTTPConnector(server.URL, server.Client()).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(records) != 25 {
		t.Fatalf("got %d records, want 25", len(records))
	}
	for _, r := range records {
		if r.Source.URL != server.URL {
			t.Fatalf("Source.URL: got %q, want %q", r.Source.URL, server.URL)
		}
	}
}

func TestFetchHTTPStatusErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   string
	}{
		{"not found", http.StatusNotFound, "404"},
		{"throttled", http.StatusTooManyRequests, "429"},
		{"server error", http.StatusInternalServerError, "500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()

			records, err := newHTTPConnector(server.URL, server.Client()).Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) || records != nil {
				t.Fatalf("got %v, %v; want error containing %q", records, err, tc.want)
			}
		})
	}
}

func TestFetchHTTPBrokenBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Items":`))
	}))
	defer server.Close()

	if _, err := newHTTPConnector(server.URL, server.Client()).Fetch(context.Background()); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestFetchInvalidURL(t *testing.T) {
	if _, err := newHTTPConnector(":bad URL", http.DefaultClient).Fetch(context.Background()); err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestFetchNetworkFailure(t *testing.T) {
	want := errors.New("connection failed")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, want
	})}
	_, err := newHTTPConnector("https://example.invalid", client).Fetch(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want wrapped transport error", err)
	}
}

func TestFetchCancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request should not reach the server")
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newHTTPConnector(server.URL, server.Client()).Fetch(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchPagination(t *testing.T) {
	items := []map[string]any{
		item(map[string]any{"savingsPlan": bothSavings("0.00000332")}),
		item(map[string]any{
			"tierMinimumUnits": num("25000"),
			"savingsPlan":      bothSavings("0.00000332"),
		}),
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/json" {
			t.Errorf("request: %s, Accept=%q", r.Method, r.Header.Get("Accept"))
		}
		page := map[string]any{"Items": items[1:]}
		if r.URL.Path == "/" {
			page["Items"] = items[:1]
			page["NextPageLink"] = "http://" + r.Host + "/next"
		} else if r.URL.Path != "/next" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(page); err != nil {
			t.Errorf("encode page: %v", err)
		}
	}))
	defer server.Close()

	got, err := newHTTPConnector(server.URL, server.Client()).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	want, err := Normalize(decodePlans(t, payload(t, items...)), server.URL, SourceType)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if requests != 2 || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %d requests and %+v, want 2 requests and %+v", requests, got, want)
	}
}

func TestFixturePricing(t *testing.T) {
	data := loadFixture(t)
	records, err := Normalize(decodePlans(t, data), SourceURL, SourceType)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(records) != 25 {
		t.Fatalf("got %d records, want 25", len(records))
	}
	if err := catalog.ValidateRecords(records); err != nil {
		t.Fatalf("ValidateRecords: %v", err)
	}
	cases := []struct {
		meter, product, sku, region, dimension, unit, start, end, amount, savings string
	}{
		{"Premium vCPU Duration", "premium", "premium", "canadacentral", "vcpu_duration", "vCPU-hour", "0", "", "0.175", "0.14525"},
		{"Always Ready Baseline", "flex-consumption", "always-ready", "southcentralus", "baseline_duration", "GB-second", "0", "", "0.000004", "0.00000332"},
		{"On Demand Total Executions", "flex-consumption", "on-demand", "francecentral", "executions", "execution", "0", "250000", "0", "0.000000332"},
		{"On Demand Total Executions", "flex-consumption", "on-demand", "francecentral", "executions", "execution", "250000", "", "0.0000004", "0.000000332"},
		{"Standard Total Executions", "consumption", "standard", "usgovarizona", "executions", "execution", "0", "1000000", "0", ""},
		{"Standard Total Executions", "consumption", "standard", "usgovarizona", "executions", "execution", "1000000", "", "0.0000002", ""},
		{"Standard Execution Time", "consumption", "standard", "southafricanorth", "execution_duration", "GB-second", "0", "400000", "0", ""},
		{"Standard Execution Time", "consumption", "standard", "southafricanorth", "execution_duration", "GB-second", "400000", "", "0.000016", ""},
		{"Always Ready Total Executions", "flex-consumption", "always-ready", "usgovtexas", "executions", "execution", "0", "", "0.0000005", ""},
		{"Always Ready Execution Time", "flex-consumption", "always-ready", "eastus", "execution_duration", "GB-second", "0", "", "0.000016", "0.00001328"},
		{"On Demand Execution Time", "flex-consumption", "on-demand", "centralindia", "execution_duration", "GB-second", "0", "100000", "0", "0.00003071"},
		{"On Demand Execution Time", "flex-consumption", "on-demand", "centralindia", "execution_duration", "GB-second", "100000", "", "0.000037", "0.00003071"},
		{"Premium Memory Duration", "premium", "premium", "australiaeast", "memory_duration", "GiB-hour", "0", "", "0.013", "0.01079"},
	}
	for _, tc := range cases {
		t.Run(tc.meter+"/"+tc.start, func(t *testing.T) {
			matching := where(records, func(r catalog.PricingRecord) bool {
				return r.Attributes["meter_name"] == tc.meter && r.Region == tc.region
			})
			base := where(matching, func(r catalog.PricingRecord) bool {
				return r.PurchaseModel == "on_demand" && r.Tier != nil && r.Tier.StartQuantity == tc.start
			})
			if len(base) != 1 {
				t.Fatalf("got %d matching base records, want 1", len(base))
			}
			r := base[0]
			if r.Product != tc.product || r.SKU != tc.sku || r.PricingDimension != tc.dimension || r.BillingUnit != tc.unit || r.Price.Amount != tc.amount || r.Tier.EndQuantity != tc.end {
				t.Errorf("unexpected base record: %+v", r)
			}
			committed := where(matching, func(r catalog.PricingRecord) bool { return r.PurchaseModel == "committed" })
			if tc.savings == "" {
				if len(committed) != 0 {
					t.Fatalf("unexpected savings records: %+v", committed)
				}
				return
			}
			if len(committed) != 2 {
				t.Fatalf("got %d savings records, want 2", len(committed))
			}
			terms := map[string]bool{}
			for _, r := range committed {
				terms[r.Dimensions["commitment_term"]] = true
				if r.Price.Amount != tc.savings || r.Tier != nil || r.Dimensions["commitment_type"] != "savings_plan" {
					t.Errorf("unexpected savings record: %+v", r)
				}
			}
			if !terms["1_year"] || !terms["3_year"] {
				t.Errorf("unexpected commitment terms: %v", terms)
			}
		})
	}
	var source AzureResponse
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		matches := 0
		for _, p := range source.Items {
			if p.MeterID != r.Attributes["meter_id"] || p.SKUID != r.ProviderSKUID || p.ArmRegionName != r.Region {
				continue
			}
			if r.PurchaseModel == "on_demand" && p.TierMinimumUnits.String() != r.Attributes["provider_tier_minimum"] {
				continue
			}
			matches++
			if r.Provider != "azure" || r.ProviderProductID != p.ProductID || r.Price.Currency != p.CurrencyCode || r.EffectiveFrom == nil || r.EffectiveFrom.Format(time.RFC3339) != p.EffectiveStartDate || r.Source != (catalog.Source{Type: SourceType, URL: SourceURL}) {
				t.Errorf("source identity or provenance mismatch: %+v", r)
			}
			retail, unit := p.RetailPrice.String(), p.UnitPrice.String()
			if r.PurchaseModel == "committed" {
				found := false
				for _, saving := range p.SavingsPlan {
					if saving.Term == r.Attributes["provider_savings_term"] {
						retail, unit = saving.RetailPrice.String(), saving.UnitPrice.String()
						found = true
					}
				}
				if !found {
					t.Errorf("missing source savings term: %+v", r)
				}
			}
			if r.Attributes["provider_retail_price"] != retail || r.Attributes["provider_unit_price"] != unit || r.Attributes["provider_unit"] != p.UnitOfMeasure {
				t.Errorf("raw source price or unit mismatch: %+v", r)
			}
		}
		if matches == 0 {
			t.Errorf("record has no source row: %+v", r)
		}
	}
}

func TestPipelineBuildAndWriteJSONRoundTrip(t *testing.T) {
	opts := pipeline.BuildOptions{Version: "test-azure-functions", GeneratedAt: time.Unix(0, 0).UTC()}
	var first bytes.Buffer
	if err := pipeline.BuildAndWriteJSON(context.Background(), newFixtureConnector(t), opts, &first); err != nil {
		t.Fatalf("BuildAndWriteJSON: %v", err)
	}
	loaded, err := catalog.LoadJSON(bytes.NewReader(first.Bytes()))
	if err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if loaded.Version != opts.Version || !loaded.GeneratedAt.Equal(opts.GeneratedAt) || len(loaded.Records) != 25 {
		t.Fatalf("unexpected snapshot: %+v", loaded)
	}
	var second bytes.Buffer
	if err := pipeline.BuildAndWriteJSON(context.Background(), newFixtureConnector(t), opts, &second); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("snapshot JSON changed between identical builds")
	}
}

func TestPipelineErrorPropagation(t *testing.T) {
	want := errors.New("source read failed")
	c := New()
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, want })
	_, err := pipeline.BuildSnapshot(context.Background(), c, pipeline.BuildOptions{Version: "test", GeneratedAt: time.Unix(0, 0).UTC()})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want wrapped source error", err)
	}
}

func TestSavingsPlanTermsKeepDistinctPrices(t *testing.T) {
	records := mustNormalize(t, item(map[string]any{"savingsPlan": []any{
		saving("1 Year", "0.00000332", "0.00000332"),
		saving("3 Years", "0.0000025", "0"),
	}}))
	want := map[string]string{"1_year": "0.000000332", "3_year": "0.00000025"}
	committed := where(records, func(r catalog.PricingRecord) bool { return r.PurchaseModel == "committed" })
	if len(committed) != 2 {
		t.Fatalf("got %d savings records, want 2", len(committed))
	}
	for _, r := range committed {
		if r.Price.Amount != want[r.Dimensions["commitment_term"]] {
			t.Errorf("unexpected term or price: %+v", r)
		}
	}
}

func TestEffectiveDatesStaySeparate(t *testing.T) {
	records := mustNormalize(t, item(nil), item(map[string]any{"effectiveStartDate": "2026-01-01T00:00:00Z", "retailPrice": num("0.000005")}))
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if err := catalog.ValidateRecords(records); err != nil {
		t.Fatal(err)
	}
	if records[0].EffectiveFrom.Equal(*records[1].EffectiveFrom) {
		t.Fatal("effective dates were merged")
	}
}

func TestOptionalProviderMetadata(t *testing.T) {
	row := item(nil)
	for _, key := range []string{"armSkuName", "location", "effectiveStartDate", "savingsPlan"} {
		delete(row, key)
	}
	records := mustNormalize(t, row)
	if len(records) != 1 || records[0].EffectiveFrom != nil || records[0].Attributes["arm_sku_name"] != "" || records[0].Attributes["location"] != "" {
		t.Fatalf("unexpected optional metadata: %+v", records)
	}
	if err := catalog.ValidateRecords(records); err != nil {
		t.Fatal(err)
	}
}

func TestFetchPaginationFailure(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"HTTP status", `{}`, http.StatusInternalServerError},
		{"malformed JSON", `{"Items":`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			c := New()
			c.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				body, status := tc.body, tc.status
				if requests == 1 {
					var page map[string]any
					if err := json.Unmarshal(payload(t, item(nil)), &page); err != nil {
						t.Fatal(err)
					}
					page["NextPageLink"] = "https://example.invalid/next"
					encoded, err := json.Marshal(page)
					if err != nil {
						t.Fatal(err)
					}
					body, status = string(encoded), http.StatusOK
				} else if r.URL.String() != "https://example.invalid/next" {
					t.Errorf("unexpected next URL: %s", r.URL)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			records, err := c.Fetch(context.Background())
			if err == nil || records != nil || requests != 2 {
				t.Fatalf("got %d requests, %d records, error %v; want second-page failure without partial records", requests, len(records), err)
			}
		})
	}
}

func TestConsumptionPaidExecutionTier(t *testing.T) {
	records, err := newFixtureConnector(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	paid := where(records, func(record catalog.PricingRecord) bool {
		return record.Product == "consumption" &&
			record.PurchaseModel == "on_demand" &&
			record.Attributes["meter_name"] == "Standard Total Executions" &&
			record.Attributes["provider_tier_minimum"] == "100000.0"
	})
	if len(paid) != 1 {
		t.Fatalf("got %d paid execution records, want 1", len(paid))
	}
	record := paid[0]
	if record.Price.Amount != "0.0000002" {
		t.Errorf("unexpected execution price: %s", record.Price.Amount)
	}
	if record.Tier == nil {
		t.Fatal("expected execution tier")
	}
	if record.Tier.StartQuantity != "1000000" {
		t.Errorf("unexpected tier start: %s", record.Tier.StartQuantity)
	}
	if record.Tier.EndQuantity != "" {
		t.Error("expected unbounded tier")
	}
}
