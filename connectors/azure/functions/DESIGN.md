# Azure Functions Connector Design

## Source

The connector integrates with the **Azure Retail Prices API**, filtered to the Azure Functions service:

- **Endpoint:** `https://prices.azure.com/api/retail/prices?api-version=2023-01-01-preview&$filter=serviceName%20eq%20%27Functions%27`
- **Source Type:** `azure_retail_prices_api`
- **API Documentation:** [Azure Retail Prices API](https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices)
- **Pricing Documentation:** [Azure Functions Pricing](https://azure.microsoft.com/en-us/pricing/details/functions/)

The Azure Retail Prices API is the official, public, machine-readable pricing catalog published by Microsoft. It requires no private credentials. The preview API version includes savings-plan rates, and the service filter is case-sensitive.

`New()` configures an `http.Client` with a 30-second timeout per request. Every request uses the caller's context, allowing cancellation during paginated retrieval. The connector does not set an overall deadline for the complete fetch.

The connector follows `NextPageLink` until it is empty or null, collecting all items before normalization so that pricing tiers split across pages remain grouped together. HTTP responses must return status 200, and response bodies are closed after decoding or immediately on a status error. Request, transport, status, decoding, and normalization failures return an error without partial records. Errors are returned without adding page-specific context. Pagination has no explicit page-count, repeated-URL, same-origin, or response-size checks.

Tests use captured pricing rows in `testdata/source.json`, injected HTTP transports, and local test servers. They do not contact Azure. `Normalize(plans, source, sourceType)` accepts decoded `[]AzurePlan` rows. To process a saved API response, decode it into `AzureResponse` and pass its `Items` field.

## Pricing Model

Azure Functions exposes separate meters for hosting plans and their billable resources:

1. **Consumption:** Execution count and resource consumption in GB-seconds. Source tiers represent monthly free grants of one million executions and 400,000 GB-seconds for eligible subscriptions, followed by paid usage.

2. **Flex Consumption, On Demand:** Execution count and execution duration. Free-grant tiers cover 250,000 executions and 100,000 GB-seconds per month for eligible subscriptions.

3. **Flex Consumption, Always Ready:** Separate meters for idle baseline resource consumption, execution duration, and execution count. No free-grant allowance is inferred for these meters.

4. **Premium:** Separate vCPU and memory duration meters. The API publishes these rates per vCPU-hour and GiB-hour. No execution-count price is inferred.

5. **Savings Plans:** Eligible meters contain nested `savingsPlan` entries for one-year and three-year commitments. Each term produces a separate committed usage rate, distinct from pay-as-you-go pricing.

Products, SKUs, regions, effective dates, resource dimensions, purchase models, and applicable tiers remain distinguishable in the canonical output.

## Mapping Decisions

| Canonical Field | Source in Azure | Description & Normalization |
|---|---|---|
| `Provider` | `"azure"` | Canonical provider identifier. |
| `Product` | `productName` | `Functions` becomes `consumption`; `Flex Consumption` becomes `flex-consumption`; `Premium Functions` becomes `premium`. |
| `SKU` | `skuName` | `Standard` becomes `standard`; `On Demand` becomes `on-demand`; `Always Ready` becomes `always-ready`; `Premium` becomes `premium`. |
| `ProviderProductID` | `productId` | Original Azure product identifier. |
| `ProviderSKUID` | `skuId` | Original Azure SKU identifier. |
| `Region` | `armRegionName` | Preserved directly, including government regions. No region allowlist or primary-meter filter is applied. An empty region is preserved by `Fetch` and `Normalize`, then rejected by generic validation when building the snapshot. |
| `PurchaseModel` | `type`, `savingsPlan` | `Consumption` rows become `on_demand`; nested savings-plan entries become `committed`. |
| `BillingUnit` | `unitOfMeasure` | Normalized to `execution`, `GB-second`, `vCPU-hour`, or `GiB-hour`. |
| `PricingDimension` | Product/SKU/meter combination | `executions`, `execution_duration`, `baseline_duration`, `vcpu_duration`, or `memory_duration`. |
| `Dimensions` | `savingsPlan.term` | Committed records retain `commitment_type: savings_plan` and `commitment_term: 1_year` or `3_year`. |
| `Price` | `retailPrice`, `currencyCode` | Exact decimal rate per canonical billing unit and the original currency. Savings-plan records use their own nested retail price. |
| `Tier` | `tierMinimumUnits` | Converted to canonical quantities. The next minimum supplies the upper boundary. Final tiers omit `EndQuantity`. |
| `EffectiveFrom` | `effectiveStartDate` | Parsed as an RFC 3339 timestamp and normalized to UTC. Missing dates remain unset. Savings-plan records inherit their parent row's effective date. |
| `EffectiveTo` | Not supplied | Left unset rather than inferred. |
| `Source` | Type and URL | `azure_retail_prices_api` with the initial filtered endpoint URL for live fetches; direct normalization uses its supplied source type and URL. |
| `Attributes` | Provider metadata | Retains meter and service identifiers and names, location, ARM SKU name, primary-meter flag, original billing unit, and original retail and unit prices. On-demand records retain the original tier minimum; committed records retain the original savings term. |

### Supported Meters

| Product | SKU | Azure Meter | Pricing Dimension | Canonical Billing Unit |
|---|---|---|---|---|
| Consumption | Standard | Standard Total Executions | `executions` | `execution` |
| Consumption | Standard | Standard Execution Time | `execution_duration` | `GB-second` |
| Flex Consumption | On Demand | On Demand Total Executions | `executions` | `execution` |
| Flex Consumption | On Demand | On Demand Execution Time | `execution_duration` | `GB-second` |
| Flex Consumption | Always Ready | Always Ready Total Executions | `executions` | `execution` |
| Flex Consumption | Always Ready | Always Ready Execution Time | `execution_duration` | `GB-second` |
| Flex Consumption | Always Ready | Always Ready Baseline | `baseline_duration` | `GB-second` |
| Premium | Premium | Premium vCPU Duration | `vcpu_duration` | `vCPU-hour` |
| Premium | Premium | Premium Memory Duration | `memory_duration` | `GiB-hour` |

Meter classification uses an explicit composite key consisting of the provider product name, SKU name, and meter name. Unsupported combinations return an error rather than being silently ignored or assigned an inferred pricing dimension.

### Exact Prices and Unit Conversion

Numbers are decoded as `json.Number` and converted using `math/big.Rat` without passing through `float64`.

The numeric billing quantity is extracted dynamically from `unitOfMeasure` as a positive `int64`, while the remaining unit text is validated against the expected provider unit.

For a source billing quantity N:

- Canonical unit price = Source retail price / N
- Canonical tier threshold = Source tier minimum x N

For example, the Consumption execution meter uses the source billing unit `10`:

| Field | Source Value | Canonical Value |
|---|---|---|
| Billing unit | `10` | `execution` |
| Retail price | `0.000002` | `0.0000002` |
| Tier minimum | `100000` | `1000000` |

This produces a price per individual execution rather than per ten executions.

Source GB-second and hourly rates with a billing quantity of one retain their original numerical values. GB-second and GiB-hour units remain distinct; no conversion between decimal and binary memory units is performed.

Rational numbers are serialized as exact decimal strings using `FloatPrec()` and `FloatString()`. Negative retail prices, negative tier thresholds, invalid billing quantities, unexpected unit text, and nonterminating decimal results are rejected.

### Tier Construction

Pricing rows are grouped using a composite key consisting of:

- Product ID
- SKU ID
- Meter ID
- Region
- Currency
- Source billing unit
- Purchase type
- Effective start date

This prevents unrelated regional, product, currency, or pricing-version records from being combined into the same tier structure.

Within each group, tier minima are sorted numerically using rational comparisons rather than lexicographical string comparisons. Duplicate tier minima are rejected.

The next tier's normalized minimum quantity becomes the current tier's upper boundary. The final tier has an unbounded upper limit, represented by an omitted `EndQuantity`.

Single on-demand rows retain their source-derived minimum quantity with an unbounded upper limit.

For example, Consumption execution pricing produces the following canonical tiers:

| Start Quantity | End Quantity | Price per Execution |
|---|---|---|
| `0` | `1000000` | `0` |
| `1000000` | Unbounded | `0.0000002` |

Free and paid tiers use the same normalization logic. No special zero-price handling or fabricated free-grant quantities are required.

### Savings Plan Normalization

Nested savings-plan entries are normalized into separate canonical records with the purchase model `committed`.

Commitment terms are represented through canonical dimensions:

| Azure Term | Canonical Commitment Term |
|---|---|
| `1 Year` | `1_year` |
| `3 Years` | `3_year` |

Each committed record also contains `commitment_type: savings_plan`.

Savings-plan prices are converted using the same billing quantity factor as their parent meter. The canonical price is calculated from the nested `retailPrice`, while both the original nested `retailPrice` and `unitPrice` are preserved in attributes.

Savings-plan rates are modeled as flat committed usage rates independently of pay-as-you-go tiers. This is a normalization assumption because the source API does not explicitly define how nested savings-plan rates interact with PAYG free-grant thresholds.

Committed records therefore do not inherit PAYG tiers or their original tier-minimum attributes.

Savings entries repeated across source tiers are deduplicated by canonical identity using `catalog.RecordKey`. Differences in normalized prices or retained provider metadata for the same identity cause normalization to fail rather than silently discard conflicting source information.

Distinct commitment terms remain separate even when their prices are identical.

### Deterministic Output

All output records are sorted by `catalog.RecordKey` before being returned. For records with unique canonical identities, output ordering does not depend on source-row order or Go map iteration. On-demand identity collisions are rejected by the generic pipeline; `Normalize` does not validate those collisions itself.

The existing generic pipeline remains responsible for canonical validation, snapshot metadata, ordering, and serialization. No provider-specific persistence, consumer API, or changes to the shared canonical schema are introduced.

## Assumptions and Limitations

- **Scope:** Supported combinations are explicitly mapped for Consumption, Flex Consumption, and Premium. Unknown product/SKU/meter combinations and unsupported purchase types fail normalization. Durable Task Scheduler capacity-unit and dispatched-action meters are not mapped. App Service hosting, storage, networking, private discounts, and other services are also outside this connector's supported scope.

- **Shared Free Grants:** Free grants are monthly subscription allowances shared across eligible applications. Canonical tiers preserve their quantities but cannot express subscription-wide pooling, billing periods, or eligibility conditions. Consumers must not apply a separate free grant to every regional record. Missing tiers are not fabricated.

- **Savings Plan Semantics:** Savings-plan rates are represented as independent committed usage rates without PAYG free-grant tiers. The connector does not calculate commitment utilization, hourly spending obligations, or the interaction between committed usage and eligible free grants.

- **Savings Plan Metadata:** Some Azure savings-plan entries report a zero `unitPrice` despite having a nonzero `retailPrice`. The connector uses `retailPrice` for canonical pricing while preserving both original values without modification. The reason for this discrepancy is not established. The one-year and three-year rates are also identical in the captured data. This is what the API returns, and the connector does not alter it.

- **Conflicting Source Records:** Duplicate tier thresholds within the same meter group are rejected. Savings-plan records sharing the same canonical identity are consolidated only when their complete normalized representations match. Metadata differences can therefore cause normalization to fail even when canonical prices are identical.

- **Deployment Costs:** Premium minimum instance allocation, billing increments, configured memory, and savings-plan hourly spend commitments are not encoded by these unit rates. A complete deployment cost calculation requires additional usage and configuration inputs.

- **Currencies and Identity:** Live requests use the API's default USD currency. Normalization retains the supplied currency, but the shared canonical key excludes currency and meter ID. Distinct rates that otherwise collide are rejected during normalization or generic validation. Multi-currency aggregation is not supported by this connector's default fetch.

- **Validation Boundary:** `Normalize` returns an empty slice without error for nil or empty input. `Fetch` rejects a retrieval with no items. The generic snapshot pipeline checks required fields such as region and currency, source URL validity, and duplicate canonical identities. `Fetch` and `Normalize` do not perform generic record validation; normalization separately checks duplicate tier minima and conflicting savings-plan identities. Regions are copied from `armRegionName` without validation or allowlisting.

- **Effective Dates:** Effective start dates are preserved from the source. Effective end dates are not supplied and are therefore not inferred. Nested savings-plan entries inherit their parent row's effective start date.

- **Mutable Source:** The endpoint returns current public rates. Fixed fixtures make tests reproducible, while separate live API captures may contain different provider metadata or prices.

- **Retrieval:** Decoded items from all pages are retained in memory before normalization. There are no explicit page-count, repeated-URL, same-origin, or response-size checks, and no retry or backoff policy. Context cancellation applies to HTTP requests; normalization itself does not accept a context.

## Testing

`testdata/source.json` contains 13 pricing rows selected from the public API capture dated 2026-11-28. Item fields and numeric spellings are preserved, while the response count is adjusted to the selected rows and the next-page link is cleared.

The fixture covers all nine supported meter mappings across multiple regions and hosting plans.

Expected normalized output:

| Record Type | Count |
|---|---:|
| Source pricing rows | 13 |
| On-demand records | 13 |
| Committed records | 12 |
| Total canonical records | 25 |

Tests use fixed fixtures, injected HTTP transports, and local HTTP servers to ensure reproducibility without contacting Azure or requiring private credentials. HTTP server tests require permission to listen on local ports.

### Test Coverage

- Connector interface compliance and the generic connector contract.
- Integration with the existing generic snapshot pipeline.
- Canonical record validation and duplicate identity detection.
- All nine supported pricing meter mappings.
- Consumption and Flex Consumption free-grant tiers.
- Exact decimal arithmetic and dynamic billing unit conversion.
- Numeric tier sorting and upper-bound construction.
- Single-tier and unbounded pricing structures.
- One-year and three-year savings-plan normalization.
- Savings-plan deduplication and conflict detection.
- Preservation of original provider prices, identifiers, and metadata.
- Regional pricing isolation and government-region coverage.
- Effective date parsing and UTC conversion.
- Deterministic output and source input-order independence.
- JSON snapshot round trips.
- Malformed and empty source responses.
- Unsupported meter/SKU combinations, purchase types, and billing units.
- Invalid prices, thresholds, and savings-plan terms.
- HTTP status errors, transport failures, and context cancellation.
- Pagination, including pricing tiers split across multiple pages.
- Pagination failures without returning partial pricing catalogs.

### Running Tests

Run the connector-specific tests:

```bash
go test ./connectors/azure/functions
```

Run tests with the race detector:

```bash
go test -race ./connectors/azure/functions
```

Run tests with coverage reporting:

```bash
go test -cover ./connectors/azure/functions
```

Run the repository-wide checks:

```bash
make check
```
