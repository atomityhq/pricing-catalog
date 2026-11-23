# Azure Managed Disks connector design

## Source

- [Azure Retail Prices API](https://prices.azure.com/api/retail/prices) is Microsoft's public retail-price feed. Microsoft documents the endpoint, its response fields, reservation examples, and pagination in the [Retail Prices API overview](https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices). This is the primary public source for the prices represented here.
- The captured regional queries use `serviceName eq 'Storage'`. The connector applies its product and type filters to records it receives.
- `New(reader io.Reader)` accepts one API JSON document. The connector makes no HTTP requests and does not follow `NextPageLink`; callers provide each page they want processed. Tests use captured records in `testdata/pricing.json`.

## Scope

The connector keeps records whose `productName` exactly matches one of these names:

- Standard HDD Managed Disks
- Standard SSD Managed Disks
- Premium SSD Managed Disks
- Premium SSD Managed Disks_v8
- Azure Premium SSD v2
- Ultra Disks

Snapshot, transaction, and burst meters under these products are retained as separate meter records. Other product names are excluded, including page blobs, Azure Elastic SAN, and other blob, file, queue, table, and data lake products.

Only `type` values `Consumption` and `Reservation` are kept. They map to `on_demand` and `reserved`, respectively; other types are skipped. The sampled records for the six included products contained these two types.

The exact product names, units, tier pairs, and six-name allowlist above are observations from the captured eastus and westus2 Retail Prices API responses. For separate disk-specific context on disk types, snapshots, transaction meters, and bursting, see Microsoft's [Managed Disks pricing page](https://azure.microsoft.com/en-us/pricing/details/managed-disks/) and [Azure Disk Storage billing documentation](https://learn.microsoft.com/en-us/azure/virtual-machines/disks-understand-billing). Those pages provide context; the precise product names and units in this connector come from the captured API records.

## Pricing model

Price records distinguish product, SKU, region, meter, billing unit, tier, and purchase model. SKU names and meter names expose differences such as disk size and redundancy (for example, LRS or ZRS). Premium SSD v2 and Ultra provide separate capacity, IOPS, or throughput meters where applicable. The API's units are retained verbatim, including `1/Month`, `1 GB/Month`, `1 GiB/Hour`, `1/Hour`, `1 GB/Hour`, `1 TB`, `10K`, and `10K/Month`.

In the sampled eastus and westus2 Premium SSD v2 records, IOPS and throughput meters each include a zero-price baseline and a paid tier. The paired rows differ in `tierMinimumUnits` (`0.0` and `3000.0` for IOPS; `0.0` and `125.0` for throughput). The tier threshold is retained as a dimension so the two prices remain distinct.

Reservation records include the API's `reservationTerm` when it is non-empty. The fixture includes P80 LRS reservations for one year and ten years. The ten-year term is observed in the API responses under `Premium SSD Managed Disks_v8`; it is not inferred from the API overview's reservation-term description.

## Mapping to the canonical model

| Canonical field | Azure field / value | Handling |
|---|---|---|
| `Provider` | constant `azure` | Fixed provider identifier. |
| `Product` | `productName` | Preserved verbatim. |
| `SKU` | `skuName` | Preserved verbatim. |
| `ProviderProductID` / `ProviderSKUID` | `productId` / `skuId` | Preserved as provider identifiers. |
| `Region` | `armRegionName` | Preserved verbatim. |
| `PurchaseModel` | `type` | `Consumption` → `on_demand`; `Reservation` → `reserved`. |
| `BillingUnit` | `unitOfMeasure` | Preserved verbatim; no conversion. |
| `PricingDimension` | `meterName` | Per-meter dimension. |
| `Price.Amount` | `retailPrice`, fallback to `unitPrice` if empty | Decoded with `json.Number`; passed through as a decimal string without floating-point conversion or rounding. |
| `Price.Currency` | `currencyCode` | Preserved verbatim; no currency conversion. |
| `EffectiveFrom` | `effectiveStartDate` | Preserved as the record effective date. |
| `Source.Type` | `azure_retail_prices` | Literal source type in the connector. |
| `Source.URL` | Retail Prices API endpoint | `https://prices.azure.com/api/retail/prices`. |

The connector puts these entries in `Dimensions`: `meterName`, `meterId`, `tierMinimumUnits`, and `itemType`. It adds `reservationTerm` only when the API value is non-empty. `tierMinimumUnits` is decoded as `json.Number` and its original numeric text is used as the dimension value. The canonical `RecordKey` includes `Dimensions`, so tier and reservation-term distinctions contribute to record identity.

Output records are sorted by `catalog.RecordKey` before `Fetch` returns.

## Assumptions

- The connector preserves the API's currency code and price text; it does not convert currencies, units, or prices. The fixture uses USD.
- `tierMinimumUnits` is stored in `Dimensions`, not the canonical `Tier` field. A free baseline is represented as a record with price zero.
- Reservation prices are preserved exactly as returned; their meaning is not inferred. In the sampled eastus P80 LRS rows, `unitOfMeasure` says `1/Month`, but the one-year and ten-year reservation amounts (`41087.0` and `410880.0`) are far above the on-demand amount (`3604.11`), so readers should not compare them directly.

## Information not represented

- `armSkuName`, `location`, `isPrimaryMeterRegion`, and service identifiers are not preserved in the canonical record.

## Known limitations

- The connector processes one supplied JSON document and does not paginate. The Retail Prices API is paginated; Microsoft documents a maximum of 1,000 records per response and provides `NextPageLink` for subsequent pages ([API pagination documentation](https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices#api-response-pagination)).
- The product allowlist is exact. A new product name is omitted until the allowlist is updated. Other `_v8` products remain excluded.
- A query filtered by `productName eq 'Premium SSD Managed Disks'` does not return the ten-year P80 reservation rows. Those rows use the distinct product name `Premium SSD Managed Disks_v8` and must be queried separately.

## Running the tests

```sh
go test ./connectors/azure/managed-disks/...
make check
make validate-example
```

## AI assistance
I wrote connector.go myself  I used AI for:

- researching how Azure prices managed disks and which fields to preserve in dimensions;
- generating the`testdata/pricing.json` and `connector_test.go`, I later verified pricing.json and made required changes;
- running audits of the connector against live Azure Retail Prices
  responses, and drafting this document.
I made the code changes that came out of those audits.
I can explain every function in this submission.
