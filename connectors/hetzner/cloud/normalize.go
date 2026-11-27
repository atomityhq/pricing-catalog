package cloud

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"pricing-catalog/pkg/catalog"
)

const (
	providerName = "hetzner"
	sourceType   = "hetzner-cloud-pricing-api"
)

// pricingResponse matches GET /v1/pricing from the Hetzner Cloud API.
type pricingResponse struct {
	Pricing pricingPayload `json:"pricing"`
}

type pricingPayload struct {
	Currency    string              `json:"currency"`
	VATRate     string              `json:"vat_rate"`
	Volume      *volumePricing      `json:"volume"`
	ServerTypes []serverTypePricing `json:"server_types"`
}

type volumePricing struct {
	PricePerGBMonth moneyPair `json:"price_per_gb_month"`
}

type serverTypePricing struct {
	ID     int64                 `json:"id"`
	Name   string                `json:"name"`
	Prices []serverLocationPrice `json:"prices"`
}

type serverLocationPrice struct {
	Location          string    `json:"location"`
	PriceHourly       moneyPair `json:"price_hourly"`
	PriceMonthly      moneyPair `json:"price_monthly"`
	IncludedTraffic   int64     `json:"included_traffic"`
	PricePerTBTraffic moneyPair `json:"price_per_tb_traffic"`
}

type moneyPair struct {
	Net   string `json:"net"`
	Gross string `json:"gross"`
}

func normalizePricing(data []byte, sourceURL string) ([]catalog.PricingRecord, error) {
	var payload pricingResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode Hetzner pricing JSON: %w", err)
	}
	if err := validatePayload(payload.Pricing); err != nil {
		return nil, err
	}

	currency := strings.TrimSpace(payload.Pricing.Currency)
	vatRate := strings.TrimSpace(payload.Pricing.VATRate)
	records := make([]catalog.PricingRecord, 0)

	for _, server := range payload.Pricing.ServerTypes {
		serverRecords, err := normalizeServerType(server, currency, vatRate, sourceURL)
		if err != nil {
			return nil, err
		}
		records = append(records, serverRecords...)
	}

	if payload.Pricing.Volume != nil {
		volumeRecord, err := normalizeVolume(*payload.Pricing.Volume, currency, vatRate, sourceURL)
		if err != nil {
			return nil, err
		}
		records = append(records, volumeRecord)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("pricing payload produced no in-scope records")
	}

	catalog.SortRecords(records)
	return records, nil
}

func validatePayload(p pricingPayload) error {
	if strings.TrimSpace(p.Currency) == "" {
		return fmt.Errorf("pricing.currency is required")
	}
	if len(p.ServerTypes) == 0 && p.Volume == nil {
		return fmt.Errorf("pricing payload must include server_types and/or volume")
	}
	return nil
}

func normalizeServerType(
	server serverTypePricing,
	currency, vatRate, sourceURL string,
) ([]catalog.PricingRecord, error) {
	name := strings.TrimSpace(server.Name)
	if name == "" {
		return nil, fmt.Errorf("server_types entry missing name")
	}
	if server.ID == 0 {
		return nil, fmt.Errorf("server type %q: id is required", name)
	}
	if len(server.Prices) == 0 {
		return nil, fmt.Errorf("server type %q: prices must not be empty", name)
	}

	productID := strconv.FormatInt(server.ID, 10)
	records := make([]catalog.PricingRecord, 0, len(server.Prices)*3)

	// Sort locations so fixture iteration order cannot affect output.
	locs := append([]serverLocationPrice(nil), server.Prices...)
	sort.SliceStable(locs, func(i, j int) bool {
		return locs[i].Location < locs[j].Location
	})

	for _, price := range locs {
		location := strings.TrimSpace(price.Location)
		if location == "" {
			return nil, fmt.Errorf("server type %q: location is required", name)
		}

		hourly, err := moneyRecord(
			"Cloud Server",
			name,
			productID,
			fmt.Sprintf("%s-%s-hourly", name, location),
			location,
			"hourly",
			"hour",
			"compute",
			price.PriceHourly,
			currency,
			vatRate,
			sourceURL,
			map[string]string{
				"server_type":            name,
				"included_traffic_bytes": strconv.FormatInt(price.IncludedTraffic, 10),
			},
			map[string]string{
				"server_type_id": productID,
				"billing_cap":    "monthly",
			},
		)
		if err != nil {
			return nil, fmt.Errorf("server type %q location %q hourly: %w", name, location, err)
		}
		records = append(records, hourly)

		monthly, err := moneyRecord(
			"Cloud Server",
			name,
			productID,
			fmt.Sprintf("%s-%s-monthly", name, location),
			location,
			"monthly",
			"month",
			"compute",
			price.PriceMonthly,
			currency,
			vatRate,
			sourceURL,
			map[string]string{
				"server_type":            name,
				"included_traffic_bytes": strconv.FormatInt(price.IncludedTraffic, 10),
			},
			map[string]string{
				"server_type_id": productID,
				"billing_cap":    "monthly",
			},
		)
		if err != nil {
			return nil, fmt.Errorf("server type %q location %q monthly: %w", name, location, err)
		}
		records = append(records, monthly)

		traffic, err := moneyRecord(
			"Traffic",
			name+"-traffic",
			productID,
			fmt.Sprintf("%s-%s-traffic", name, location),
			location,
			"on_demand",
			"TB",
			"egress",
			price.PricePerTBTraffic,
			currency,
			vatRate,
			sourceURL,
			map[string]string{
				"server_type":            name,
				"included_traffic_bytes": strconv.FormatInt(price.IncludedTraffic, 10),
			},
			map[string]string{
				"server_type_id": productID,
				"overage":        "true",
			},
		)
		if err != nil {
			return nil, fmt.Errorf("server type %q location %q traffic: %w", name, location, err)
		}
		records = append(records, traffic)
	}

	return records, nil
}

func normalizeVolume(
	volume volumePricing,
	currency, vatRate, sourceURL string,
) (catalog.PricingRecord, error) {
	return moneyRecord(
		"Volume",
		"volume",
		"volume",
		"volume-gb-month",
		"global",
		"on_demand",
		"GB-month",
		"storage",
		volume.PricePerGBMonth,
		currency,
		vatRate,
		sourceURL,
		nil,
		map[string]string{
			"resource": "block_storage",
		},
	)
}

func moneyRecord(
	product, sku, productID, skuID, region, purchaseModel, billingUnit, dimension string,
	amount moneyPair,
	currency, vatRate, sourceURL string,
	dimensions, attributes map[string]string,
) (catalog.PricingRecord, error) {
	net := strings.TrimSpace(amount.Net)
	if net == "" {
		return catalog.PricingRecord{}, fmt.Errorf("net price is required")
	}
	gross := strings.TrimSpace(amount.Gross)

	dims := cloneMap(dimensions)
	attrs := cloneMap(attributes)
	if vatRate != "" {
		attrs["vat_rate"] = vatRate
	}
	if gross != "" {
		attrs["price_gross"] = gross
	}

	return catalog.PricingRecord{
		Provider:          providerName,
		Product:           product,
		SKU:               sku,
		ProviderProductID: productID,
		ProviderSKUID:     skuID,
		Region:            region,
		PurchaseModel:     purchaseModel,
		BillingUnit:       billingUnit,
		PricingDimension:  dimension,
		Dimensions:        dims,
		Price: catalog.Money{
			Amount:   net,
			Currency: currency,
		},
		Source: catalog.Source{
			Type: sourceType,
			URL:  sourceURL,
		},
		Attributes: attrs,
	}, nil
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
