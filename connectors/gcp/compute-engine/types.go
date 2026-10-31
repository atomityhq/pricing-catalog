package gcpcomputeengine

import "encoding/json"

// gcpResponse represents a paginated response from the Google Cloud Billing
// Catalog API v1 services.skus.list endpoint.
type gcpResponse struct {
	Skus          []gcpSKU `json:"skus"`
	NextPageToken string   `json:"nextPageToken"`
}

// gcpSKU represents a single billable SKU within Google Cloud Compute Engine.
type gcpSKU struct {
	Name                string           `json:"name"`
	SKUID               string           `json:"skuId"`
	Description         string           `json:"description"`
	Category            gcpCategory      `json:"category"`
	ServiceRegions      []string         `json:"serviceRegions"`
	PricingInfo         []gcpPricingInfo `json:"pricingInfo"`
	ServiceProviderName string           `json:"serviceProviderName"`
	GeoTaxonomy         gcpGeoTaxonomy   `json:"geoTaxonomy"`
}

// gcpCategory classifies a SKU into a resource family, group, and usage model.
type gcpCategory struct {
	ServiceDisplayName string `json:"serviceDisplayName"`
	ResourceFamily     string `json:"resourceFamily"`
	ResourceGroup      string `json:"resourceGroup"`
	UsageType          string `json:"usageType"`
}

// gcpPricingInfo contains the effective timestamp, rate expression, and
// aggregation rules for a SKU price entry.
type gcpPricingInfo struct {
	EffectiveTime          string               `json:"effectiveTime"`
	Summary                string               `json:"summary"`
	PricingExpression      gcpPricingExpression `json:"pricingExpression"`
	AggregationInfo        gcpAggregationInfo   `json:"aggregationInfo"`
	CurrencyConversionRate json.Number          `json:"currencyConversionRate"`
}

// gcpPricingExpression defines the billing units and tiered unit rates for a SKU.
type gcpPricingExpression struct {
	UsageUnit                string        `json:"usageUnit"`
	DisplayQuantity          json.Number   `json:"displayQuantity"`
	TieredRates              []gcpTierRate `json:"tieredRates"`
	UsageUnitDescription     string        `json:"usageUnitDescription"`
	BaseUnit                 string        `json:"baseUnit"`
	BaseUnitDescription      string        `json:"baseUnitDescription"`
	BaseUnitConversionFactor json.Number   `json:"baseUnitConversionFactor"`
}

// gcpTierRate represents one quantity bracket within a tiered pricing expression.
type gcpTierRate struct {
	StartUsageAmount json.Number `json:"startUsageAmount"`
	UnitPrice        gcpMoney    `json:"unitPrice"`
}

// gcpMoney represents a monetary value split into whole units and nano fractions (10^-9).
type gcpMoney struct {
	CurrencyCode string `json:"currencyCode"`
	Units        string `json:"units"`
	Nanos        int32  `json:"nanos"`
}

// gcpAggregationInfo describes the level and interval over which usage is aggregated.
type gcpAggregationInfo struct {
	AggregationLevel    gcpAggregationLevel    `json:"aggregationLevel"`
	AggregationInterval gcpAggregationInterval `json:"aggregationInterval"`
	AggregationCount    int                    `json:"aggregationCount"`
}

// gcpAggregationLevel defines the entity level at which usage tiers are evaluated.
type gcpAggregationLevel string

// gcpAggregationInterval defines the time window over which usage tiers are evaluated.
type gcpAggregationInterval string

// gcpGeoTaxonomy describes the geographic scope and region list for a SKU.
type gcpGeoTaxonomy struct {
	Type    gcpType  `json:"type"`
	Regions []string `json:"regions"`
}

// gcpType represents the geographic taxonomy classification (e.g. REGIONAL, GLOBAL).
type gcpType string
