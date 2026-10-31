package gcpcomputeengine

import "encoding/json"

type gcpResponse struct {
	Skus          []gcpSKU `json:"skus"`
	NextPageToken string   `json:"nextPageToken"`
}

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

type gcpCategory struct {
	ServiceDisplayName string `json:"serviceDisplayName"`
	ResourceFamily     string `json:"resourceFamily"`
	ResourceGroup      string `json:"resourceGroup"`
	UsageType          string `json:"usageType"`
}

type gcpPricingInfo struct {
	EffectiveTime          string               `json:"effectiveTime"`
	Summary                string               `json:"summary"`
	PricingExpression      gcpPricingExpression `json:"pricingExpression"`
	AggregationInfo        gcpAggregationInfo   `json:"aggregationInfo"`
	CurrencyConversionRate json.Number          `json:"currencyConversionRate"`
}

type gcpPricingExpression struct {
	UsageUnit                string        `json:"usageUnit"`
	DisplayQuantity          json.Number   `json:"displayQuantity"`
	TieredRates              []gcpTierRate `json:"tieredRates"`
	UsageUnitDescription     string        `json:"usageUnitDescription"`
	BaseUnit                 string        `json:"baseUnit"`
	BaseUnitDescription      string        `json:"baseUnitDescription"`
	BaseUnitConversionFactor json.Number   `json:"baseUnitConversionFactor"`
}

type gcpTierRate struct {
	StartUsageAmount json.Number `json:"startUsageAmount"`
	UnitPrice        gcpMoney    `json:"unitPrice"`
}

type gcpMoney struct {
	CurrencyCode string `json:"currencyCode"`
	Units        string `json:"units"`
	Nanos        int32  `json:"nanos"`
}

type gcpAggregationInfo struct {
	AggregationLevel    gcpAggregationLevel    `json:"aggregationLevel"`
	AggregationInterval gcpAggregationInterval `json:"aggregationInterval"`
	AggregationCount    int                    `json:"aggregationCount"`
}

type gcpAggregationLevel string
type gcpAggregationInterval string

const (
	aggregationLevelUnspecified gcpAggregationLevel = "AGGREGATION_LEVEL_UNSPECIFIED"
	aggregationLevelAccount     gcpAggregationLevel = "ACCOUNT"
	aggregationLevelProject     gcpAggregationLevel = "PROJECT"
)

const (
	aggregationIntervalUnspecified gcpAggregationInterval = "AGGREGATION_INTERVAL_UNSPECIFIED"
	aggregationIntervalDaily       gcpAggregationInterval = "DAILY"
	aggregationIntervalMonthly     gcpAggregationInterval = "MONTHLY"
)

type gcpGeoTaxonomy struct {
	Type    gcpType  `json:"type"`
	Regions []string `json:"regions"`
}

type gcpType string

const (
	typeUnspecified   gcpType = "TYPE_UNSPECIFIED"
	typeGlobal        gcpType = "GLOBAL"
	typeRegional      gcpType = "REGIONAL"
	typeMultiRegional gcpType = "MULTI_REGIONAL"
)
