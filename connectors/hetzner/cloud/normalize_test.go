package cloud

import (
	"bytes"
	"strings"
	"testing"

	"pricing-catalog/pkg/catalog"
)

func requireNormalizeError(t *testing.T, data []byte, want string) {
	t.Helper()
	records, err := normalizePricing(data, defaultURL)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("normalizePricing error = %v, want containing %q", err, want)
	}
	if records != nil {
		t.Fatalf("invalid input returned %d partial records", len(records))
	}
}

func TestNormalizePricingFixture(t *testing.T) {
	records, err := normalizePricing(readFixture(t), defaultURL)
	if err != nil {
		t.Fatalf("normalizePricing: %v", err)
	}
	if len(records) != 7 {
		t.Fatalf("got %d records, want 7", len(records))
	}
	if err := catalog.ValidateRecords(records); err != nil {
		t.Fatalf("ValidateRecords: %v", err)
	}

	hourly := findRecord(t, records, "cx23", "fsn1", "hourly", "compute")
	if hourly.Product != "Cloud Server" || hourly.Provider != "hetzner" {
		t.Fatalf("hourly identity: %+v", hourly)
	}
	if hourly.ProviderProductID != "114" {
		t.Fatalf("cx23 provider id: %q", hourly.ProviderProductID)
	}
	if hourly.Price.Amount != "0.0088000000" || hourly.Price.Currency != "EUR" {
		t.Fatalf("hourly price: %+v", hourly.Price)
	}
	if hourly.BillingUnit != "hour" || hourly.PurchaseModel != "hourly" {
		t.Fatalf("hourly billing: %+v", hourly)
	}
	if hourly.Attributes["price_gross"] != "0.0104720000" || hourly.Attributes["vat_rate"] != "19.00" {
		t.Fatalf("hourly attributes: %+v", hourly.Attributes)
	}
	if hourly.Dimensions["included_traffic_bytes"] != "21990232555520" {
		t.Fatalf("hourly included traffic: %+v", hourly.Dimensions)
	}

	monthly := findRecord(t, records, "cx23", "fsn1", "monthly", "compute")
	if monthly.Price.Amount != "5.4900000000" || monthly.BillingUnit != "month" {
		t.Fatalf("monthly record: %+v", monthly)
	}

	caxHourly := findRecord(t, records, "cax11", "fsn1", "hourly", "compute")
	if caxHourly.Price.Amount != "0.0096000000" || caxHourly.ProviderProductID != "45" {
		t.Fatalf("cax11 hourly: %+v", caxHourly)
	}
	caxMonthly := findRecord(t, records, "cax11", "fsn1", "monthly", "compute")
	if caxMonthly.Price.Amount != "5.9900000000" {
		t.Fatalf("cax11 monthly: %+v", caxMonthly)
	}

	trafficEU := findRecord(t, records, "cx23-traffic", "fsn1", "on_demand", "egress")
	if trafficEU.Price.Amount != "1.0000000000" || trafficEU.BillingUnit != "TB" {
		t.Fatalf("EU traffic: %+v", trafficEU)
	}

	volume := findRecord(t, records, "volume", "global", "on_demand", "storage")
	if volume.Product != "Volume" || volume.BillingUnit != "GB-month" {
		t.Fatalf("volume record: %+v", volume)
	}
	if volume.Price.Amount != "0.0572000000" {
		t.Fatalf("volume price: %+v", volume.Price)
	}
}

func TestNormalizePreservesHourlyMonthlyAndLocations(t *testing.T) {
	records, err := normalizePricing(readFixture(t), defaultURL)
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, record := range records {
		if record.Product != "Cloud Server" {
			continue
		}
		key := record.SKU + "|" + record.Region + "|" + record.PurchaseModel
		seen[key] = true
	}

	for _, key := range []string{
		"cx23|fsn1|hourly",
		"cx23|fsn1|monthly",
		"cax11|fsn1|hourly",
		"cax11|fsn1|monthly",
	} {
		if !seen[key] {
			t.Fatalf("missing server pricing distinction %q", key)
		}
	}
	for _, key := range []string{
		"cx23|ash|hourly",
		"cx23|sin|hourly",
		"cpx11|fsn1|monthly",
	} {
		if seen[key] {
			t.Fatalf("unexpected unverified location/SKU still present: %q", key)
		}
	}
}

func TestNormalizeIsDeterministic(t *testing.T) {
	data := readFixture(t)
	first, err := normalizePricing(data, defaultURL)
	if err != nil {
		t.Fatal(err)
	}
	second, err := normalizePricing(bytes.Clone(data), defaultURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("record counts differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if catalog.RecordKey(first[i]) != catalog.RecordKey(second[i]) {
			t.Fatalf("record order diverged at %d", i)
		}
		if first[i].Price != second[i].Price {
			t.Fatalf("price diverged at %d: %+v vs %+v", i, first[i].Price, second[i].Price)
		}
	}
}

func TestNormalizeRejectsMalformedJSON(t *testing.T) {
	for _, input := range []string{"", "{", `{"pricing":`, "[]", "42"} {
		requireNormalizeError(t, []byte(input), "decode Hetzner pricing JSON")
	}
	// encoding/json accepts bare null into a struct value; validation then rejects it.
	requireNormalizeError(t, []byte("null"), "pricing.currency is required")
}

func TestNormalizeRejectsMissingCurrency(t *testing.T) {
	data := editFixture(t, func(root map[string]any) {
		pricing := root["pricing"].(map[string]any)
		delete(pricing, "currency")
	})
	requireNormalizeError(t, data, "pricing.currency is required")
}

func TestNormalizeRejectsEmptyServerName(t *testing.T) {
	data := editFixture(t, func(root map[string]any) {
		pricing := root["pricing"].(map[string]any)
		servers := pricing["server_types"].([]any)
		server := servers[0].(map[string]any)
		server["name"] = " "
	})
	requireNormalizeError(t, data, "missing name")
}

func TestNormalizeRejectsMissingNetPrice(t *testing.T) {
	data := editFixture(t, func(root map[string]any) {
		pricing := root["pricing"].(map[string]any)
		volume := pricing["volume"].(map[string]any)
		price := volume["price_per_gb_month"].(map[string]any)
		delete(price, "net")
	})
	requireNormalizeError(t, data, "net price is required")
}

func TestNormalizeRejectsEmptyScope(t *testing.T) {
	data := editFixture(t, func(root map[string]any) {
		pricing := root["pricing"].(map[string]any)
		delete(pricing, "server_types")
		delete(pricing, "volume")
	})
	requireNormalizeError(t, data, "must include server_types and/or volume")
}

func findRecord(
	t *testing.T,
	records []catalog.PricingRecord,
	sku, region, purchaseModel, dimension string,
) catalog.PricingRecord {
	t.Helper()
	for _, record := range records {
		if record.SKU == sku &&
			record.Region == region &&
			record.PurchaseModel == purchaseModel &&
			record.PricingDimension == dimension {
			return record
		}
	}
	t.Fatalf("no record for sku=%q region=%q model=%q dimension=%q", sku, region, purchaseModel, dimension)
	return catalog.PricingRecord{}
}
