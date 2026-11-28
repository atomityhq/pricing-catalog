package functions

import "encoding/json"

type AzureResponse struct {
	Items        []AzurePlan `json:"Items"`
	NextPageLink string      `json:"NextPageLink"`
}

type AzurePlan struct {
	CurrencyCode     string      `json:"currencyCode"`
	TierMinimumUnits json.Number `json:"tierMinimumUnits"`
	RetailPrice      json.Number `json:"retailPrice"`
	UnitPrice        json.Number `json:"unitPrice"`

	ArmRegionName        string `json:"armRegionName"`
	Location             string `json:"location"`
	IsPrimaryMeterRegion bool   `json:"isPrimaryMeterRegion"`

	EffectiveStartDate string `json:"effectiveStartDate"`

	MeterID   string `json:"meterId"`
	MeterName string `json:"meterName"`

	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`

	SKUID      string `json:"skuId"`
	SKUName    string `json:"skuName"`
	ArmSKUName string `json:"armSkuName"`

	ServiceName   string `json:"serviceName"`
	ServiceID     string `json:"serviceId"`
	ServiceFamily string `json:"serviceFamily"`

	UnitOfMeasure string `json:"unitOfMeasure"`
	Type          string `json:"type"`

	SavingsPlan []AzureSavingsPlan `json:"savingsPlan"`
}

type AzureSavingsPlan struct {
	UnitPrice   json.Number `json:"unitPrice"`
	RetailPrice json.Number `json:"retailPrice"`
	Term        string      `json:"term"`
}

type MeterGroupKey struct {
	ProductID     string
	SKUID         string
	MeterID       string
	Region        string
	Currency      string
	Unit          string
	Type          string
	EffectiveDate string
}
