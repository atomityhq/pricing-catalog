package gcppersistentdisk

import (
	"encoding/json"
	"testing"
)

func TestFormatGCPMoney(t *testing.T) {
	cases := []struct {
		name    string
		money   gcpMoney
		want    string
		wantErr bool
	}{
		{"zero nanos", gcpMoney{CurrencyCode: "USD", Units: "0", Nanos: 0}, "0.0000", false},
		{"forty cents per thousand", gcpMoney{CurrencyCode: "USD", Units: "0", Nanos: 40000000}, "0.0400", false},
		{"exact nanos", gcpMoney{CurrencyCode: "USD", Units: "0", Nanos: 4954950}, "0.00495495", false},
		{"whole units plus nanos", gcpMoney{CurrencyCode: "USD", Units: "1", Nanos: 500000000}, "1.5000", false},
		{"empty units defaults to zero", gcpMoney{CurrencyCode: "USD", Units: "", Nanos: 170000000}, "0.1700", false},
		{"negative units", gcpMoney{CurrencyCode: "USD", Units: "-1", Nanos: 0}, "", true},
		{"negative nanos", gcpMoney{CurrencyCode: "USD", Units: "0", Nanos: -1}, "", true},
		{"nanos out of range", gcpMoney{CurrencyCode: "USD", Units: "0", Nanos: 1000000000}, "", true},
		{"invalid units", gcpMoney{CurrencyCode: "USD", Units: "abc", Nanos: 0}, "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := formatGCPMoney(tc.money)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got %q", got)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBuildTier(t *testing.T) {
	rates := []gcpTierRate{
		{StartUsageAmount: json.Number("0"), UnitPrice: gcpMoney{CurrencyCode: "USD", Units: "0", Nanos: 0}},
		{StartUsageAmount: json.Number("30"), UnitPrice: gcpMoney{CurrencyCode: "USD", Units: "0", Nanos: 40000000}},
	}

	first, err := buildTier(rates, 0)
	if err != nil {
		t.Fatalf("build first tier: %v", err)
	}
	if first.StartQuantity != "0" || first.EndQuantity != "30" {
		t.Errorf("expected first tier [0, 30), got [%s, %s)", first.StartQuantity, first.EndQuantity)
	}

	last, err := buildTier(rates, 1)
	if err != nil {
		t.Fatalf("build last tier: %v", err)
	}
	if last.StartQuantity != "30" || last.EndQuantity != "" {
		t.Errorf("expected last tier [30, open), got [%s, %s)", last.StartQuantity, last.EndQuantity)
	}

	if _, err := buildTier([]gcpTierRate{{StartUsageAmount: json.Number("-1")}}, 0); err == nil {
		t.Error("expected error on negative startUsageAmount, got nil")
	}
}

func TestNormalizeBillingUnit(t *testing.T) {
	cases := map[string]string{
		"h":         "hour",
		"GiBy.h":    "GiB-hour",
		"GiBy.mo":   "GiB-month",
		"GiBy.d":    "GiB-day",
		"mo":        "month",
		"d":         "day",
		"operation": "operation",
		"GiB":       "GiB",
		"":          "unit",
	}
	for in, want := range cases {
		if got := normalizeBillingUnit(in); got != want {
			t.Errorf("normalizeBillingUnit(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsSupportedScope(t *testing.T) {
	sku := func(family, group, desc string) gcpSKU {
		return gcpSKU{
			Description: desc,
			Category:    gcpCategory{ResourceFamily: family, ResourceGroup: group, UsageType: "OnDemand"},
		}
	}

	cases := []struct {
		name string
		sku  gcpSKU
		want bool
	}{
		{"standard disk by full name", sku("Storage", "PD", "Standard Persistent Disk Capacity in Iowa"), true},
		{"ssd disk by pd token", sku("Storage", "PD", "SSD backed PD Capacity in Iowa"), true},
		{"regional disk by pd token", sku("Storage", "PD", "Regional Balanced PD Capacity in Iowa"), true},
		{"extreme iops by pd token", sku("Storage", "PD", "Extreme PD Provisioned IOPS in Iowa"), true},
		{"snapshot by snapshot token", sku("Storage", "Snapshot", "Standard Snapshot Storage in Iowa"), true},
		{"archive snapshot", sku("Storage", "Snapshot", "Archive Snapshot Storage in Iowa"), true},
		{"hyperdisk excluded", sku("Storage", "SSD", "Hyperdisk Balanced IOPS Confidential Mode in Doha"), false},
		{"hyperdisk storage pool excluded", sku("Storage", "HDBSP", "Hyperdisk Balanced Storage Pools Standard IOPS - Oregon"), false},
		{"local ssd excluded", sku("Storage", "LocalSSD", "Local SSD backed Local Storage in Iowa"), false},
		{"network excluded", sku("Network", "VPNInterregionIngress", "Network Vpn Inter Region Data Transfer In from Paris to Netherlands"), false},
		{"compute excluded", sku("Compute", "CPU", "Sole Tenancy Premium for C4 Sole Tenancy Instance Core running in Santiago"), false},
		{"custom image storage excluded", sku("Storage", "Image", "Custom Image Storage in Iowa"), false},
		{"license excluded", sku("License", "Google", "Licensing Fee for Windows Server 2012 BYOL (CPU cost)"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSupportedScope(tc.sku); got != tc.want {
				t.Errorf("isSupportedScope(%q) = %v, want %v", tc.sku.Description, got, tc.want)
			}
		})
	}
}

func TestResolveProductAndDimension(t *testing.T) {
	sku := func(desc string) gcpSKU {
		return gcpSKU{
			Description: desc,
			Category:    gcpCategory{ResourceFamily: "Storage", ResourceGroup: "PD", UsageType: "OnDemand"},
		}
	}

	cases := []struct {
		desc      string
		product   string
		dimension string
		repl      string
	}{
		{"Standard Persistent Disk Capacity in Iowa", "Persistent Disk", "standard", "zonal"},
		{"SSD backed PD Capacity in Iowa", "Persistent Disk", "ssd", "zonal"},
		{"Regional Balanced PD Capacity in Iowa", "Persistent Disk", "balanced", "regional"},
		{"Extreme PD Capacity in Iowa", "Persistent Disk", "extreme", "zonal"},
		{"Extreme PD Provisioned IOPS in Iowa", "Persistent Disk", "extreme", "zonal"},
		{"Standard Snapshot Storage in Iowa", "Persistent Disk Snapshot", "standard_snapshot", "regional"},
		{"Archive Snapshot Storage in Iowa", "Persistent Disk Snapshot", "archive_snapshot", "regional"},
		{"Archive Snapshot Retrieval in Iowa", "Persistent Disk Snapshot", "archive_snapshot", "regional"},
		{"Instant Snapshot Creation in Iowa", "Persistent Disk Snapshot", "instant_snapshot", "zonal"},
		{"Standard Snapshot Storage Multi-Region in the US", "Persistent Disk Snapshot", "standard_snapshot", "multi_regional"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			s := sku(tc.desc)
			if got := resolveProduct(s); got != tc.product {
				t.Errorf("resolveProduct = %q, want %q", got, tc.product)
			}
			if got := resolvePricingDimension(s); got != tc.dimension {
				t.Errorf("resolvePricingDimension = %q, want %q", got, tc.dimension)
			}
			if got := extractDimensions(s)["replication"]; got != tc.repl {
				t.Errorf("replication = %q, want %q", got, tc.repl)
			}
		})
	}

	// The Extreme IOPS SKU must be flagged as provisioned IOPS.
	iopsDims := extractDimensions(sku("Extreme PD Provisioned IOPS in Iowa"))
	if iopsDims["iops"] != "provisioned" {
		t.Errorf("expected iops=provisioned, got %#v", iopsDims)
	}
	if iopsDims["disk_type"] != "pd-extreme" {
		t.Errorf("expected disk_type=pd-extreme, got %#v", iopsDims)
	}

	// The archive retrieval SKU must be flagged as a retrieval charge.
	retrievalDims := extractDimensions(sku("Archive Snapshot Retrieval in Iowa"))
	if retrievalDims["charge"] != "retrieval" {
		t.Errorf("expected charge=retrieval, got %#v", retrievalDims)
	}
	if retrievalDims["snapshot_type"] != "archive" {
		t.Errorf("expected snapshot_type=archive, got %#v", retrievalDims)
	}
}

func TestHasWordToken(t *testing.T) {
	if !hasWordToken("storage pd capacity in iowa", "pd") {
		t.Error("expected pd token in 'storage pd capacity in iowa'")
	}
	if hasWordToken("update capacity in iowa", "pd") {
		t.Error("expected no pd token in 'update capacity in iowa'")
	}
	if !hasWordToken("standard snapshot storage", "snapshot") {
		t.Error("expected snapshot token in 'standard snapshot storage'")
	}
}
