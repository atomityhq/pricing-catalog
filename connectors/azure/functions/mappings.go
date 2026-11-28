package functions

import "fmt"

type MeterKey struct {
	Product string
	SKU     string
	Meter   string
}

type MeterMapping struct {
	PricingDimension string
	BillingUnit      string
	ProviderUnit     string
}

var productMappings = map[string]string{
	"Functions":         "consumption",
	"Flex Consumption":  "flex-consumption",
	"Premium Functions": "premium",
}

var skuMappings = map[string]string{
	"Standard":     "standard",
	"On Demand":    "on-demand",
	"Always Ready": "always-ready",
	"Premium":      "premium",
}

var meterMappings = map[MeterKey]MeterMapping{
	// Consumption
	{"Functions", "Standard", "Standard Total Executions"}: {
		"executions", "execution", "",
	},
	{"Functions", "Standard", "Standard Execution Time"}: {
		"execution_duration", "GB-second", "GB Second",
	},

	// Flex Consumption — On Demand
	{"Flex Consumption", "On Demand", "On Demand Total Executions"}: {
		"executions", "execution", "",
	},
	{"Flex Consumption", "On Demand", "On Demand Execution Time"}: {
		"execution_duration", "GB-second", "GB Second",
	},

	// Flex Consumption — Always Ready
	{"Flex Consumption", "Always Ready", "Always Ready Total Executions"}: {
		"executions", "execution", "",
	},
	{"Flex Consumption", "Always Ready", "Always Ready Execution Time"}: {
		"execution_duration", "GB-second", "GB Second",
	},
	{"Flex Consumption", "Always Ready", "Always Ready Baseline"}: {
		"baseline_duration", "GB-second", "GB Second",
	},

	// Premium
	{"Premium Functions", "Premium", "Premium vCPU Duration"}: {
		"vcpu_duration", "vCPU-hour", "Hour",
	},
	{"Premium Functions", "Premium", "Premium Memory Duration"}: {
		"memory_duration", "GiB-hour", "GiB Hour",
	},
}

var purchaseModelMappings = map[string]string{
	"Consumption": "on_demand",
}

var savingsTermMappings = map[string]string{
	"1 Year":  "1_year",
	"3 Years": "3_year",
}

func getMeterMapping(plan AzurePlan) (MeterMapping, error) {
	key := MeterKey{
		Product: plan.ProductName,
		SKU:     plan.SKUName,
		Meter:   plan.MeterName,
	}

	mapping, ok := meterMappings[key]
	if !ok {
		return MeterMapping{}, fmt.Errorf(
			"unsupported pricing combination: %+v", key,
		)
	}

	return mapping, nil
}
