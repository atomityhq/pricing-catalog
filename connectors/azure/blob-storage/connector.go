package blobstorage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
	"strconv"
	"time"
)

const URL = "https://prices.azure.com/api/retail/prices?$filter=serviceName%20eq%20%27Storage%27%20and%20productName%20eq%20%27Blob%20Storage%27"

type source struct {
	BillingCurrency    string `json:"BillingCurrency"`
	CustomerEntityID   string `json:"CustomerEntityId"`
	CustomerEntityType string `json:"CustomerEntityType"`
	Items              []struct {
		CurrencyCode         string      `json:"currencyCode"`
		TierMinimumUnits     json.Number `json:"tierMinimumUnits"`
		RetailPrice          json.Number `json:"retailPrice"`
		UnitPrice            json.Number `json:"unitPrice"`
		ARMRegionName        string      `json:"armRegionName"`
		Location             string      `json:"location"`
		EffectiveStartDate   time.Time   `json:"effectiveStartDate"`
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
		IsPrimaryMeterRegion bool        `json:"isPrimaryMeterRegion"`
		ARMSKUName           string      `json:"armSkuName"`
	} `json:"items"`
	NextPageLink *string `json:"NextPageLink"`
	Count        int     `json:"Count"`
}

type Connector struct {
	client *http.Client
}

func New() Connector {
	return Connector{client: &http.Client{}}
}

func (c Connector) Name() string { return "azure-blobstorage" }

func (c Connector) Fetch(ctx context.Context) ([]catalog.PricingRecord, error) {
	var firstPage string = URL
	var nextPage *string = &firstPage

	var records []catalog.PricingRecord
	for nextPage != nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, *nextPage, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Add("Accept", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, &connector.SourceError{Connector: c.Name(), Err: err}
		}

		if resp.StatusCode != http.StatusOK {
			return nil, &connector.SourceError{
				Connector: c.Name(),
				Err:       fmt.Errorf("request to azure pricing API returned status code: %d", resp.StatusCode),
			}
		}

		var raw source
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			resp.Body.Close()
			return nil, err
		}

		for _, item := range raw.Items {
			records = append(records, catalog.PricingRecord{
				Provider: "azure",
				Product:  item.ProductName,
				SKU:      item.SKUName,

				ProviderProductID: item.ProductID,
				ProviderSKUID:     item.SKUID,

				Region:        item.ARMRegionName,
				PurchaseModel: item.Type,
				BillingUnit:   item.UnitOfMeasure,

				PricingDimension: item.MeterName,
				Dimensions:       nil,

				Price: catalog.Money{
					Amount:   item.RetailPrice.String(),
					Currency: item.CurrencyCode,
				},
				Tier: &catalog.Tier{
					StartQuantity: item.TierMinimumUnits.String(),
					EndQuantity:   "",
				},

				EffectiveFrom: &item.EffectiveStartDate,
				EffectiveTo:   nil,

				Source: catalog.Source{
					Type: "azure_retail_prices",
					URL:  URL,
				},
				Attributes: map[string]string{
					"meter_id":                item.MeterID,
					"service_id":              item.ServiceID,
					"service_name":            item.ServiceName,
					"service_family":          item.ServiceFamily,
					"location":                item.Location,
					"arm_sku_name":            item.ARMSKUName,
					"is_primary_meter_region": strconv.FormatBool(item.IsPrimaryMeterRegion),
				},
			})
		}

		resp.Body.Close()
		nextPage = raw.NextPageLink
	}

	return records, nil
}
