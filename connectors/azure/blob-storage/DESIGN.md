# Azure Blob Storage connector design

## Source

The connector fetches Azure Blob Storage pricing data from the [Azure Retail Prices API](https://prices.azure.com/api/retail/prices?$filter=serviceName%20eq%20%27Storage%27%20and%20productName%20eq%20%27Blob%20Storage%27), which provides unauthenticated access to retail rates for all Azure services.

The `serviceName` and `productName` filters are used to retrieve pricing data specifically for Azure Blob Storage.

## Pricing model

The Azure API provides:

- Customer and billing context:
  - Billing currency
  - Customer entity ID
  - Customer entity type

- Pricing details:
  - Currency
  - Minimum units for tier
  - Unit of measure
  - Retail price
  - Unit price
  - Region
  - Effective start date

- Product and service identification:
  - Product name and ID
  - SKU name and ID
  - Service name, ID, family

## Mapping decisions

`Dimensions` is kept as `nil` for now. `json.Number` is used to decode floating-point values, matching `PricingRecord`'s string type for price values.

For tiers, only the minimum units are provided, so end quantity in `Tier` is kept as `""` for now.

The following additional information is preserved in `Attributes`:

- Meter ID
- Service ID
- Service name
- Service family
- Location
- ARM SKU name
- Primary meter region status

## Limitations

The connector does not map end quantity in tier, resource dimensions and effective end date.
