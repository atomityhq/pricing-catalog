package azuremanageddisks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// retailPriceItem is one row of the Azure Retail Prices API.
//
// Numeric fields are json.Number, so prices keep the exact text published by
// Azure and never pass through float64.
//
// Reference: https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices
type retailPriceItem struct {
	CurrencyCode         string      `json:"currencyCode"`
	TierMinimumUnits     json.Number `json:"tierMinimumUnits"`
	RetailPrice          json.Number `json:"retailPrice"`
	ARMRegionName        string      `json:"armRegionName"`
	Location             string      `json:"location"`
	EffectiveStartDate   string      `json:"effectiveStartDate"`
	EffectiveEndDate     string      `json:"effectiveEndDate"`
	MeterID              string      `json:"meterId"`
	MeterName            string      `json:"meterName"`
	ProductID            string      `json:"productId"`
	SKUID                string      `json:"skuId"`
	ProductName          string      `json:"productName"`
	SKUName              string      `json:"skuName"`
	ServiceName          string      `json:"serviceName"`
	ServiceID            string      `json:"serviceId"`
	ServiceFamily        string      `json:"serviceFamily"`
	UnitOfMeasure        string      `json:"unitOfMeasure"`
	Type                 string      `json:"type"`
	IsPrimaryMeterRegion *bool       `json:"isPrimaryMeterRegion"`
	ARMSKUName           string      `json:"armSkuName"`
	ReservationTerm      string      `json:"reservationTerm"`
}

// retailPricePage is one page of the Azure Retail Prices API response.
// Items is a pointer so a missing "Items" field (nil) can be told apart from
// an empty list.
type retailPricePage struct {
	Items        *[]retailPriceItem `json:"Items"`
	NextPageLink string             `json:"NextPageLink"`
}

// decodePage reads one API page. A page without an "Items" array is an error,
// so a broken response never looks like an empty or partial catalog.
func decodePage(r io.Reader) (*retailPricePage, error) {
	var page retailPricePage
	if err := json.NewDecoder(r).Decode(&page); err != nil {
		return nil, fmt.Errorf("decode Azure retail prices JSON: %w", err)
	}
	if page.Items == nil {
		return nil, errors.New(`response has no "Items" array`)
	}
	return &page, nil
}
