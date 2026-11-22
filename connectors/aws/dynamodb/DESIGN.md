# AWS DynamoDB Pricing Connector Design

## Scope and authoritative source

**Package:** `connectors/aws/dynamodb`
**Connector name:** `aws-dynamodb`

The connector consumes the public AWS Price List Bulk API offer for Amazon DynamoDB:

- Current offer: <https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonDynamoDB/current/index.json>
- AWS offer-file structure: <https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/reading-service-price-list-file-for-services.html>
- AWS Price List Bulk API: <https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/using-the-aws-price-list-bulk-api.html>

The source is public and does not require AWS credentials. The connector supports the `OnDemand` and `Reserved` term categories in this offer. It emits one canonical record per source price dimension and currency. The shared catalog pipeline remains responsible for snapshot construction and validation.

The connector includes every product in the AmazonDynamoDB offer that can be assigned a valid region, including 38 change-data-capture entries that have no `productFamily` but are identified by `group` as `DDB-Kinesis` or `DDB-ElasticViews`. It does not import products from separate AWS offers such as AWS Backup, Amazon CloudWatch, or Amazon DynamoDB Accelerator (DAX).

## Source model and parsing

The AWS document has a `products` section keyed by SKU and a `terms` section grouped by category, SKU, and term key. Each term contains a map of price dimensions keyed by rate code. Product and term entries are joined by SKU; the source term key, `offerTermCode`, rate code, and currency are retained separately.

`source.go` uses `json.Decoder` tokens to traverse the top-level sections and decode product/term entries. It does not decode the entire offer into a generic nested object. The decoder retains the product map and normalized source-term slice because the source lists all products before terms and the connector's current normalization function accepts those two collections. Price-dimension map keys are sorted before records are emitted. Unknown top-level sections and unsupported future term categories are skipped. Terms without a matching product are skipped because they cannot be normalized safely.

## Canonical mapping

| Canonical field     | Mapping                                                                                                                                                       |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Provider`          | `aws`                                                                                                                                                         |
| `Product`           | `Amazon DynamoDB`                                                                                                                                             |
| `SKU`               | AWS product SKU                                                                                                                                               |
| `ProviderProductID` | AWS product SKU                                                                                                                                               |
| `ProviderSKUID`     | Composite of source term key, `offerTermCode`, rate code, and currency                                                                                        |
| `Region`            | `attributes.regionCode`; the single global vector-search SKU with `location=Any` and `usagetype=Global-VectorSearch` maps to `global`                         |
| `PurchaseModel`     | `on_demand` for `OnDemand`; `reserved` for `Reserved`                                                                                                         |
| `BillingUnit`       | Original non-empty AWS unit, unchanged (for example `ReadCapacityUnit-Hrs`, `GB-Mo`, `Requests`, or `Quantity`)                                               |
| `PricingDimension`  | Stable semantic classification based on product family, group, usage type, volume type, unit, and rate description                                            |
| `Dimensions`        | All non-empty product attributes, with keys converted to snake_case and source values preserved                                                               |
| `Price`             | Exact `pricePerUnit` decimal string and uppercase currency key; one record per currency                                                                       |
| `Tier`              | Source `beginRange` and `endRange`; `Inf` becomes an empty end. A flat `0`-to-`Inf` dimension has no tier                                                     |
| `EffectiveFrom`     | Parsed RFC3339 `effectiveDate`, normalized to UTC                                                                                                             |
| `EffectiveTo`       | Not set; the offer does not provide a per-record end date                                                                                                     |
| `Source`            | Type `aws_price_list`; URL is the current AmazonDynamoDB offer URL                                                                                            |
| `Attributes`        | Source term key, offer term code, rate code, description, original unit/ranges, and term attributes such as lease length, purchase option, and offering class |

Prices are never converted through floating point. Zero-priced source dimensions are retained. Tier boundaries are retained as decimal strings, including finite free-tier/paid-tier boundaries.

### Pricing-dimension classification

The implementation uses the following stable categories where the source provides enough metadata:

- `read_request`, `write_request`, `vector_search`, `vector_write`, and `replicated_write_request` for request-based throughput.
- `read_capacity`, `write_capacity`, and `replicated_write_capacity` for provisioned/committed capacity.
- `indexed_storage`, `indexed_storage_ia`, `pitr_backup_storage`, and `backup_storage` for storage meters.
- `import_data`, `export_data`, and `restore_data` for data movement.
- `streams_read_request` for DynamoDB Streams reads.
- `change_data_capture` for the Kinesis Data Streams and AWS Glue Elastic Views integration meters in the DynamoDB offer.
- `upfront_fee` for the separate `Quantity`/upfront price dimension in a Reserved term.
- `storage` or `dynamodb_usage` as explicit fallbacks for otherwise unclassified non-empty source meters.

A Reserved term with a partial-upfront purchase option may contain both an hourly recurring dimension and a one-time `Quantity` dimension. Only the latter is classified as `upfront_fee`; the recurring capacity dimension keeps its read/write capacity classification. The charges are never blended or amortized.

### Region exceptions

The inspected AWS publication contains one regionless global vector-search product (`location=Any`, `usagetype=Global-VectorSearch`) and two test-region products (`location=TST`). The global SKU maps to `Region=global`. The two TST products are excluded. Any other missing or unsupported region is an error rather than an inferred region. This avoids fabricating a geographic region from a display label.

## Observed source publication

The supplied publication was dated 2026-11-11 and contained 1,121 products, 1,121 OnDemand SKU term entries, and 98 Reserved term entries (1,219 term entries total). It contained 1,486 source price dimensions across both term categories. After excluding the two TST test products, normalization produced 1,484 records. The publication's prices were USD; the implementation still iterates all currencies in `pricePerUnit` rather than hard-coding USD.

The publication included 38 entries with no `productFamily`. Their `group` and `operation` attributes identify them as DynamoDB change-data-capture meters for Kinesis or Elastic Views, so they are retained rather than silently dropped.

## Determinism, validation, and errors

- Sort price-dimension keys and currency keys before record creation.
- Sort the final records with `catalog.SortRecords`.
- Construct fresh `Dimensions` and `Attributes` maps for every record.
- Validate every record and reject duplicate canonical identities with `catalog.ValidateRecords`.
- Preserve exact amount strings and source units.
- Return contextual errors for malformed term data, invalid effective dates, missing prices, unsupported purchase categories, and unresolvable regions.
- Use context-aware HTTP requests, a bounded client timeout, non-2xx status checks, and response-body closure.
- Tests use a deterministic local fixture and `httptest`; they do not call AWS.

## Limitations and assumptions

- The `current` offer URL is mutable. Fixtures pin representative source data for repeatable tests.
- This is public list pricing, not negotiated/private pricing, a customer bill, or a cost estimate based on usage.
- Only `OnDemand` and `Reserved` categories are supported. Unknown future categories are skipped.
- Global is represented as the literal region value `global` because the canonical schema requires a non-empty region string and has no separate global-scope field.
- The two TST entries are treated as non-production test catalog entries based on their source metadata.
- Product attributes are preserved generically, but the connector does not interpret every possible AWS attribute as an independent billing dimension.
- The parser retains a product map and a term slice in memory; it avoids a second full generic JSON tree but is not a fully streaming record normalizer.
- The source publication inspected contained USD only. Multi-currency handling is implemented but is not exercised by that publication.
