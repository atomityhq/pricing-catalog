package azuremanageddisks

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"pricing-catalog/pkg/catalog"
)

// findRecord returns the single record with the given identity.
func findRecord(t *testing.T, records []catalog.PricingRecord, sku, region, dimension, purchase, tierStart string) catalog.PricingRecord {
	t.Helper()
	var found []catalog.PricingRecord
	for _, r := range records {
		start := ""
		if r.Tier != nil {
			start = r.Tier.StartQuantity
		}
		if r.SKU == sku && r.Region == region && r.PricingDimension == dimension &&
			r.PurchaseModel == purchase && start == tierStart {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d records for %s/%s/%s/%s/%q, want 1", len(found), sku, region, dimension, purchase, tierStart)
	}
	return found[0]
}

func mustTime(t *testing.T, s string) *time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return &v
}

func TestPremiumSSDOnDemandDisk(t *testing.T) {
	records := fixtureRecords(t, "retail_prices.json")
	got := findRecord(t, records, "DZH318Z0BP04/00DJ", "westeurope", "disk", "on_demand", "")

	want := catalog.PricingRecord{
		Provider:          "azure",
		Product:           "Premium SSD Managed Disks",
		SKU:               "DZH318Z0BP04/00DJ",
		ProviderProductID: "DZH318Z0BP04",
		ProviderSKUID:     "DZH318Z0BP04/00DJ",
		Region:            "westeurope",
		PurchaseModel:     "on_demand",
		BillingUnit:       "month",
		PricingDimension:  "disk",
		Dimensions: map[string]string{
			"disk_type":     "premium_ssd",
			"redundancy":    "lrs",
			"disk_tier":     "P30",
			"disk_size_gib": "1024",
		},
		Price:         catalog.Money{Amount: "148.68", Currency: "USD"},
		EffectiveFrom: mustTime(t, "2018-05-01T00:00:00Z"),
		Source:        catalog.Source{Type: "azure_retail_prices_api", URL: "https://prices.azure.com/api/retail/prices"},
		Attributes: map[string]string{
			"meter_id":                got.Attributes["meter_id"],
			"meter_name":              "P30 LRS Disk",
			"sku_name":                "P30 LRS",
			"arm_sku_name":            "Premium_SSD_Managed_Disks_P30",
			"price_type":              "Consumption",
			"unit_of_measure":         "1/Month",
			"location":                "EU West",
			"service_name":            "Storage",
			"service_id":              "DZH317F1HKN0",
			"service_family":          "Storage",
			"is_primary_meter_region": "true",
		},
	}
	if got.Attributes["meter_id"] == "" {
		t.Fatal("meter_id not preserved")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("record mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSameSKUDistinctMetersAndRedundancy(t *testing.T) {
	records := fixtureRecords(t, "retail_prices.json")

	// The disk and its shared-disk mount charge share an Azure SKU but are
	// different prices.
	mount := findRecord(t, records, "DZH318Z0BP04/00DJ", "westeurope", "disk_mount", "on_demand", "")
	if mount.Price.Amount != "8.541" || mount.BillingUnit != "month" {
		t.Fatalf("disk mount = %#v", mount)
	}

	zrs := findRecord(t, records, "DZH318Z0BP04/02DX", "westeurope", "disk", "on_demand", "")
	if zrs.Dimensions["redundancy"] != "zrs" || zrs.Price.Amount != "222.99" {
		t.Fatalf("ZRS disk = %#v", zrs)
	}

	eastus := findRecord(t, records, "DZH318Z0BP04/00CS", "eastus", "disk", "on_demand", "")
	if eastus.Price.Amount != "135.17" {
		t.Fatalf("eastus P30 price = %s, want region-specific 135.17", eastus.Price.Amount)
	}
}

func TestReservationsAreTotalTermPrices(t *testing.T) {
	records := fixtureRecords(t, "retail_prices.json")

	oneYear := findRecord(t, records, "DZH318Z0BP04/0197", "westeurope", "disk", "reserved", "")
	if oneYear.BillingUnit != "1-year" ||
		oneYear.Price.Amount != "1695.0" ||
		oneYear.Dimensions["commitment_term"] != "1_year" ||
		oneYear.Attributes["price_basis"] != "total_for_term" ||
		oneYear.Attributes["reservation_term"] != "1 Year" ||
		oneYear.Attributes["unit_of_measure"] != "1/Month" {
		t.Fatalf("1-year reservation = %#v", oneYear)
	}

	tenYear := findRecord(t, records, "DZH318XZM2Z0/0092", "westeurope", "disk", "reserved", "")
	if tenYear.BillingUnit != "10-year" ||
		tenYear.Product != "Premium SSD Managed Disks_v8" ||
		tenYear.Dimensions["disk_type"] != "premium_ssd" ||
		tenYear.Dimensions["commitment_term"] != "10_year" ||
		tenYear.Attributes["is_primary_meter_region"] != "false" {
		t.Fatalf("10-year reservation = %#v", tenYear)
	}

	onDemand := findRecord(t, records, "DZH318Z0BP04/00DJ", "westeurope", "disk", "on_demand", "")
	if _, ok := onDemand.Dimensions["commitment_term"]; ok {
		t.Fatal("on-demand record carries a commitment term")
	}
}

func TestPremiumSSDv2TieredPrices(t *testing.T) {
	records := fixtureRecords(t, "retail_prices.json")
	const sku = "DZH318Z09V07/0004"

	for _, tc := range []struct {
		dimension, start, end, amount string
	}{
		// Tier bounds keep Azure's own text ("3000.0"), like prices do.
		{"provisioned_iops", "0.0", "3000.0", "0.0"},
		{"provisioned_iops", "3000.0", "", "0.000008"},
		{"provisioned_throughput", "0.0", "125.0", "0.0"},
		{"provisioned_throughput", "125.0", "", "0.000065"},
	} {
		r := findRecord(t, records, sku, "westeurope", tc.dimension, "on_demand", tc.start)
		if r.Tier == nil || r.Tier.EndQuantity != tc.end || r.Price.Amount != tc.amount || r.BillingUnit != "hour" {
			t.Fatalf("%s tier %s: %#v tier=%#v", tc.dimension, tc.start, r, r.Tier)
		}
	}

	capacity := findRecord(t, records, sku, "westeurope", "provisioned_capacity", "on_demand", "")
	if capacity.Tier != nil || capacity.BillingUnit != "GiB-hour" || capacity.Price.Amount != "0.00013" {
		t.Fatalf("flat capacity price = %#v", capacity)
	}
	if capacity.Dimensions["disk_type"] != "premium_ssd_v2" || capacity.Dimensions["redundancy"] != "lrs" {
		t.Fatalf("dimensions = %#v", capacity.Dimensions)
	}
}

func TestComponentMeters(t *testing.T) {
	records := fixtureRecords(t, "retail_prices.json")

	for _, tc := range []struct {
		sku, dimension, unit, amount string
	}{
		{"DZH318Z0BP04/00PG", "snapshot", "GB-month", "0.145"},
		{"DZH318Z0BP04/01VG", "burst_enablement", "month", "31.949"},
		{"DZH318Z0BP04/01VC", "burst_transactions", "10K-month", "0.0065"},
		{"DZH318Z0BP04/04WP", "confidential_compute_encryption", "GiB-hour", "0.00003"},
		{"DZH318Z0BP0L/02CK", "transactions", "10K", "0.00065"},
		{"DZH318Z0BP0L/00CF", "transactions", "10K", "0.0005"},
		{"DZH318Z0BP68/0006", "provisioned_iops", "hour", "0.000088"},
		{"DZH318Z0BP68/0006", "provisioned_throughput", "hour", "0.000572"},
		{"DZH318Z0BP68/0006", "vcpu_reservation", "hour", "0.0078"},
		{"DZH318Z0BP68/003Q", "instant_access_snapshot", "GB-hour", "0.000213"},
		{"DZH318Z0BP68/003Q", "instant_access_snapshot_restore", "TB", "0.572"},
	} {
		r := findRecord(t, records, tc.sku, "westeurope", tc.dimension, "on_demand", "")
		if r.BillingUnit != tc.unit || r.Price.Amount != tc.amount {
			t.Errorf("%s %s = unit %q amount %q, want %q %q", tc.sku, tc.dimension, r.BillingUnit, r.Price.Amount, tc.unit, tc.amount)
		}
	}

	// The older per-tier operations meter is kept and flagged as non-primary.
	legacy := findRecord(t, records, "DZH318Z0BP0L/00CF", "westeurope", "transactions", "on_demand", "")
	if legacy.Attributes["is_primary_meter_region"] != "false" || legacy.Dimensions["disk_tier"] != "S10" {
		t.Fatalf("legacy operations meter = %#v", legacy)
	}
}

func TestFreeAllowances(t *testing.T) {
	records := fixtureRecords(t, "retail_prices.json")

	free := findRecord(t, records, "DZH318Z0BP04/00MG", "global", "disk", "on_demand", "")
	if free.Dimensions["offer"] != "free" || free.Price.Amount != "0.0" || free.Dimensions["disk_size_gib"] != "64" {
		t.Fatalf("free P6 disk = %#v", free)
	}

	// One Azure meter shared by two SKUs must stay two records.
	e4 := findRecord(t, records, "DZH318Z0BP88/00M7", "global", "transactions", "on_demand", "")
	e6 := findRecord(t, records, "DZH318Z0BP88/00MG", "global", "transactions", "on_demand", "")
	if e4.Attributes["meter_id"] != e6.Attributes["meter_id"] || e4.Dimensions["offer"] != "free" {
		t.Fatalf("shared free meter: e4=%#v e6=%#v", e4, e6)
	}
}

func TestClassifyMeter(t *testing.T) {
	for _, tc := range []struct {
		meter, want string
		free        bool
	}{
		{"P30 LRS Disk", "disk", false},
		{"E4 Disks - Free", "disk", true},
		{"P6 LRS Disk - Free", "disk", true},
		{"P30 LRS Disk Mount", "disk_mount", false},
		{"Disk Operations - Free", "transactions", true},
		{"LRS Snapshots", "snapshot", false},
		{"Snapshots ZRS Snapshots", "snapshot", false},
		{"Instant Access Snapshots LRS", "instant_access_snapshot", false},
		{"Instant Access Snapshots LRS Disk Capacity Restored", "instant_access_snapshot_restore", false},
		{"Confidential Compute Encryption ZRS Provisioned Capacity", "confidential_compute_encryption", false},
		{"Ultra LRS Reservation per vCPU Provisioned", "vcpu_reservation", false},
		{"Some Brand New Meter", "meter_some_brand_new_meter", false},
	} {
		got, free := classifyMeter(tc.meter)
		if got != tc.want || free != tc.free {
			t.Errorf("classifyMeter(%q) = %q,%v want %q,%v", tc.meter, got, free, tc.want, tc.free)
		}
	}
}

func TestBillingUnitOf(t *testing.T) {
	for raw, want := range map[string]string{
		"1/Month":    "month",
		"1/Hour":     "hour",
		"1 GB/Month": "GB-month",
		"1 GiB/Hour": "GiB-hour",
		"1 GB/Hour":  "GB-hour",
		"1 TB":       "TB",
		"10K":        "10K",
		"10K/Month":  "10K-month",
	} {
		got, err := billingUnitOf(raw)
		if err != nil || got != want {
			t.Errorf("billingUnitOf(%q) = %q, %v want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "1", " "} {
		if _, err := billingUnitOf(raw); err == nil {
			t.Errorf("billingUnitOf(%q) accepted", raw)
		}
	}
}

func TestParseReservationTerm(t *testing.T) {
	for raw, want := range map[string][2]string{
		"1 Year":   {"1_year", "1-year"},
		"3 Years":  {"3_year", "3-year"},
		"10 Years": {"10_year", "10-year"},
	} {
		dim, unit, err := parseReservationTerm(raw)
		if err != nil || dim != want[0] || unit != want[1] {
			t.Errorf("parseReservationTerm(%q) = %q, %q, %v", raw, dim, unit, err)
		}
	}
	for _, raw := range []string{"forever", "6 Months", ""} {
		if _, _, err := parseReservationTerm(raw); err == nil {
			t.Errorf("parseReservationTerm(%q) accepted", raw)
		}
	}
}

func validItem() retailPriceItem {
	return retailPriceItem{
		CurrencyCode:       "USD",
		TierMinimumUnits:   "0.0",
		RetailPrice:        "1.5",
		ARMRegionName:      "westeurope",
		EffectiveStartDate: "2024-01-01T00:00:00Z",
		MeterID:            "meter-1",
		MeterName:          "P1 LRS Disk",
		ProductID:          "PROD",
		SKUID:              "PROD/0001",
		ProductName:        "Premium SSD Managed Disks",
		SKUName:            "P1 LRS",
		UnitOfMeasure:      "1/Month",
		Type:               "Consumption",
	}
}

func TestNormalizeRejectsBadItems(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*retailPriceItem)
		want   string
	}{
		{"missing region", func(i *retailPriceItem) { i.ARMRegionName = "" }, "armRegionName is required"},
		{"missing sku", func(i *retailPriceItem) { i.SKUID = "" }, "skuId is required"},
		{"unknown price type", func(i *retailPriceItem) { i.Type = "Spot" }, "unsupported price type"},
		{"dev/test price type", func(i *retailPriceItem) { i.Type = "DevTestConsumption" }, "unsupported price type"},
		{"reservation without term", func(i *retailPriceItem) { i.Type = "Reservation" }, "reservation term"},
		{"bad date", func(i *retailPriceItem) { i.EffectiveStartDate = "yesterday" }, "effectiveStartDate"},
		{"missing unit", func(i *retailPriceItem) { i.UnitOfMeasure = "" }, "unitOfMeasure"},
		{"bad tier minimum", func(i *retailPriceItem) { i.TierMinimumUnits = "abc" }, "invalid tierMinimumUnits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := validItem()
			tc.mutate(&item)
			_, err := normalize([]retailPriceItem{item}, sourceType, SourceURL)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestNormalizeRejectsDuplicateTier(t *testing.T) {
	a, b := validItem(), validItem()
	_, err := normalize([]retailPriceItem{a, b}, sourceType, SourceURL)
	if err == nil || !strings.Contains(err.Error(), "duplicate tier") {
		t.Fatalf("err = %v, want duplicate tier error", err)
	}
}

func TestNormalizeKeepsEffectiveEndDateAndSkipsOtherProducts(t *testing.T) {
	ended := validItem()
	ended.EffectiveEndDate = "2025-01-01T00:00:00Z"
	blob := validItem()
	blob.ProductName = "Premium Page Blob"

	records, err := normalize([]retailPriceItem{ended, blob}, sourceType, SourceURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if records[0].EffectiveTo == nil || !records[0].EffectiveTo.Equal(*mustTime(t, "2025-01-01T00:00:00Z")) {
		t.Fatalf("EffectiveTo = %v", records[0].EffectiveTo)
	}
	if records[0].Dimensions["disk_size_gib"] != "4" {
		t.Fatalf("P1 size = %q", records[0].Dimensions["disk_size_gib"])
	}
}

// The connector passes prices, currencies and tier bounds through unchanged;
// the generic pipeline validation rejects bad values.
func TestPipelineRejectsBadValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"missing price", func(m map[string]any) { delete(m, "retailPrice") }, "price.amount is required"},
		{"negative price", func(m map[string]any) { m["retailPrice"] = json.Number("-1") }, "price.amount must be a non-negative decimal"},
		{"bad currency", func(m map[string]any) { m["currencyCode"] = "US" }, "price.currency"},
		{"negative tier", func(m map[string]any) { m["tierMinimumUnits"] = json.Number("-5") }, "tier.start_quantity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(validItem())
			if err != nil {
				t.Fatal(err)
			}
			item := map[string]any{}
			if err := json.Unmarshal(raw, &item); err != nil {
				t.Fatal(err)
			}
			tc.mutate(item)
			data, err := json.Marshal(map[string]any{"Items": []any{item}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = buildSnapshot(data)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestDecodePage(t *testing.T) {
	page, err := decodePage(strings.NewReader(`{"Items":[{"skuId":"S/1","retailPrice":1.25}],"NextPageLink":"next"}`))
	if err != nil {
		t.Fatal(err)
	}
	items := *page.Items
	if len(items) != 1 || items[0].RetailPrice.String() != "1.25" || page.NextPageLink != "next" {
		t.Fatalf("page = %#v", page)
	}

	empty, err := decodePage(strings.NewReader(`{"Items":[]}`))
	if err != nil || len(*empty.Items) != 0 {
		t.Fatalf("an empty Items list is valid: %#v, %v", empty, err)
	}
	if _, err := decodePage(strings.NewReader(`{"Count":0}`)); err == nil {
		t.Fatal("page without Items accepted")
	}
}
