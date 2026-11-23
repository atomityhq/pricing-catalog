package azure_disk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"time"

	"pricing-catalog/pkg/catalog"
)

type source struct {
	BillingCurrency    string `json:"BillingCurrency"`
	CustomerEntityID   string `json:"CustomerEntityId"`
	CustomerEntityType string `json:"CustomerEntityType"`
	Items              []struct {
		CurrencyCode         string      `json:"currencyCode"`
		TierMinimumUnits     json.Number `json:"tierMinimumUnits"`
		RetailPrice          json.Number `json:"retailPrice"`
		UnitPrice            json.Number `json:"unitPrice"`
		ArmRegionName        string      `json:"armRegionName"`
		Location             string      `json:"location"`
		EffectiveStartDate   time.Time   `json:"effectiveStartDate"`
		MeterID              string      `json:"meterId"`
		MeterName            string      `json:"meterName"`
		ProductID            string      `json:"productId"`
		SkuID                string      `json:"skuId"`
		ProductName          string      `json:"productName"`
		SkuName              string      `json:"skuName"`
		ServiceName          string      `json:"serviceName"`
		ServiceID            string      `json:"serviceId"`
		ServiceFamily        string      `json:"serviceFamily"`
		UnitOfMeasure        string      `json:"unitOfMeasure"`
		Type                 string      `json:"type"`
		ReservationTerm      string      `json:"reservationTerm"`
		IsPrimaryMeterRegion bool        `json:"isPrimaryMeterRegion"`
		ArmSkuName           string      `json:"armSkuName"`
	} `json:"Items"`
}

var diskProducts = []string{
	"Standard HDD Managed Disks",
	"Standard SSD Managed Disks",
	"Premium SSD Managed Disks",
	"Azure Premium SSD v2",
	"Ultra Disks",
	"Premium SSD Managed Disks_v8",
}

type Connector struct {
	reader io.Reader
}

func New(reader io.Reader) Connector {
	return Connector{reader: reader}
}

func (c Connector) Name() string { return "azure-managed-disks" }

func (c Connector) Fetch(ctx context.Context) ([]catalog.PricingRecord, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var raw source
	dec := json.NewDecoder(c.reader)
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode source: %w", err)
	}

	records := make([]catalog.PricingRecord, 0)

	for _, item := range raw.Items {

		if !slices.Contains(diskProducts, item.ProductName) {
			continue
		}

		if item.Type != "Consumption" && item.Type != "Reservation" {
			continue
		}

		dimension := map[string]string{
			"meterName":        item.MeterName,
			"tierMinimumUnits": item.TierMinimumUnits.String(),
			"itemType":         item.Type,
			"meterId":          item.MeterID,
		}
		if item.ReservationTerm != "" {
			dimension["reservationTerm"] = item.ReservationTerm
		}

		amount := item.RetailPrice.String()
		if amount == "" {
			amount = item.UnitPrice.String()
		}

		itemtype := item.Type
		switch item.Type {
		case "Consumption":
			itemtype = "on_demand"
		case "Reservation":
			itemtype = "reserved"
		default:
			return nil, fmt.Errorf("unexpected type: %s", itemtype)
		}

		records = append(records, catalog.PricingRecord{
			Provider: "azure",
			Product:  item.ProductName,
			SKU:      item.SkuName,

			ProviderProductID: item.ProductID,
			ProviderSKUID:     item.SkuID,

			Region:        item.ArmRegionName,
			PurchaseModel: itemtype,
			BillingUnit:   item.UnitOfMeasure,

			PricingDimension: item.MeterName,
			Dimensions:       dimension,

			Price: catalog.Money{Amount: amount, Currency: item.CurrencyCode},
			//Tier : no valid Tier
			EffectiveFrom: &item.EffectiveStartDate,
			Source: catalog.Source{
				Type: "azure_retail_prices",
				URL:  "https://prices.azure.com/api/retail/prices",
			},
		})
	}
	//sorting based on record key
	//makes sure  the ouptut is a determined sequence
	sort.Slice(records, func(i, j int) bool {
		return catalog.RecordKey(records[i]) < catalog.RecordKey(records[j])
	})
	return records, nil
}
