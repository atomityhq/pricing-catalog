# Azure Managed Disks connector

Package `azuremanageddisks`, connector name `azure-managed-disks`.

## Source

The connector uses the Azure Retail Prices API:
https://prices.azure.com/api/retail/prices
([docs](https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices)).

I chose it because it is Microsoft's official price list, it is public with no
API key, and it returns JSON with stable IDs (`productId`, `skuId`, `meterId`).
The Azure pricing pages show the same data, but scraping HTML would be less
reliable.

The live connector sends this filter and follows `NextPageLink` until the last
page (about 12 pages of 1,000 items):

```text
serviceName eq 'Storage' and (contains(productName, 'Managed Disks')
  or productName eq 'Azure Premium SSD v2' or productName eq 'Ultra Disks')
```

The API returned HTTP 429 (too many requests) several times while I was
researching, so a page that gets 429 or a 5xx error is tried up to 3 times,
waiting a little longer each time. Other errors, and a response without an
`Items` array, stop the fetch: no partial catalog is produced. The live fetch
uses the API default currency, USD. Microsoft notes that filter values are
case-sensitive (the filter above uses the exact product names).

Tests do not use the network. `New(io.Reader)` reads one saved API page. The
live path is tested with `httptest`. Any currency in the source is passed
through unchanged; `testdata/retail_prices_eur.json` shows this with EUR.

## Pricing model

I looked at every in-scope item for all 70 regions (11,259 items, November 2026).

- Products: Standard HDD, Standard SSD and Premium SSD Managed Disks (plus a
  `Premium SSD Managed Disks_v8` list), Azure Premium SSD v2, and Ultra Disks.
- SKU: `skuId`, with a readable `skuName` such as `P30 LRS`. The `skuId` is
  different in each region, and reservations have their own `skuId`.
- Meter: what is actually billed. One SKU can have several meters (for example
  `P30 LRS Disk` and `P30 LRS Disk Mount`), and one meter can appear under more
  than one SKU (the free `Disk Operations - Free` meter is under E4 and E6).
- Region: `armRegionName`, plus `Global` for free allowances.
- Purchase model: `Consumption` (pay as you go) or `Reservation` with a
  `reservationTerm`. Microsoft documents only 1-year disk reservations. The API
  also has a `_v8` list with `10 Years` terms, but all of those items are
  non-primary meters (see Assumptions).
- Units: `1/Month`, `1 GB/Month`, `1 GiB/Hour`, `1 GB/Hour`, `1/Hour`, `10K`,
  `10K/Month`, `1 TB`.
- Tiers: each tier is a separate item with only a lower bound
  (`tierMinimumUnits`). Premium SSD v2 IOPS (first 3,000 free) and throughput
  (first 125 MBps free) are tiered.

How the disks are priced:

- Standard HDD (S), Standard SSD (E) and Premium SSD (P) have fixed sizes and
  a price per disk per month. LRS and ZRS have different prices. Extra meters:
  - transactions, per 10,000, for Standard SSD and Standard HDD only;
  - `Disk Mount`, a monthly charge for each additional mount of a shared
    Premium SSD or Standard SSD disk;
  - on-demand bursting for Premium SSD P30 and larger: a flat enablement fee
    (listed per month) plus a fee per 10,000 burst transactions;
  - snapshots (per GB-month) and confidential compute encryption (per GiB-hour).
- Premium SSD v2 and Ultra Disk charge for capacity, IOPS and throughput
  separately. Ultra also has a per-vCPU charge. Both have Instant Access
  snapshot meters.
- Reservations (Premium SSD P30 to P80): the price is the total for one disk
  for the whole term, even though the unit says `1/Month`. Example, P30 LRS in
  westeurope: 148.68 per month on demand, 1695.0 for a 1-year reservation.
  148.68 * 12 is about 1,784, and 1,695 is about 5% less. The API prices are
  about 5% lower for every reservation size I checked; Microsoft does not
  publish the percentage.

## Mapping decisions

| Field | Mapping |
|---|---|
| `Provider` | `azure` |
| `Product` | `productName` as published |
| `SKU`, `ProviderSKUID` | `skuId` |
| `ProviderProductID` | `productId` |
| `Region` | `armRegionName` in lower case (`Global` becomes `global`) |
| `PurchaseModel` | `Consumption` = `on_demand`, `Reservation` = `reserved` (same word as the AWS EC2 connector). Other types (for example `DevTestConsumption`, which has no disk prices) are an error. |
| `BillingUnit` | `month`, `hour`, `GB-month`, `GiB-hour`, `GB-hour`, `TB`, `10K`, `10K-month`. Reservations use the term: `1-year`, `10-year`. |
| `PricingDimension` | From `meterName`: `disk`, `disk_mount`, `transactions`, `snapshot`, `burst_enablement`, `burst_transactions`, `confidential_compute_encryption`, `provisioned_capacity`, `provisioned_iops`, `provisioned_throughput`, `vcpu_reservation`, `instant_access_snapshot`, `instant_access_snapshot_restore`. An unknown meter is kept as `meter_<name>`. |
| `Dimensions` | `disk_type`, `redundancy` (`lrs`/`zrs`), `disk_tier` (`P30`), `disk_size_gib`, `commitment_term` (`1_year`, same key as the GCP connector), `offer: free` for free meters |
| `Price` | `retailPrice` kept as the exact text Azure sends (`1695.0`, `133.4710`). No float64. The pipeline validates the format. |
| `Tier` | Items of the same meter, SKU, region, purchase model, currency, unit and dates are sorted by `tierMinimumUnits`. A tier ends where the next one starts and the last one has no end. One item that starts at 0 is a flat price (no tier). Bounds keep Azure's text, like prices (`0.0` to `3000.0`). |
| `EffectiveFrom` / `EffectiveTo` | `effectiveStartDate` / `effectiveEndDate` |
| `Source` | `azure_retail_prices_api`, the API URL |
| `Attributes` | `meter_id`, `meter_name`, `sku_name`, `arm_sku_name`, `price_type`, `reservation_term`, `unit_of_measure`, `location`, `service_*`, `is_primary_meter_region`, and `price_basis: total_for_term` on reservations |

On the full live data, every source item becomes exactly one record (11,259
in, 11,259 out). Nothing is dropped or merged and there are no duplicate keys.

Scope: an item is used if its `productName` starts with one of the five disk
product names. Prefix matching also picks up lists like `..._v8`. Everything
else is skipped, including `Premium Page Blob`, which uses the same names
(`P30 LRS`) and even the same prices, but is for unmanaged disks (retired on
March 31, 2026). That is why the scope check uses `productName`, not
`skuName`. The fixture contains one such item and a test checks that it is
skipped.

## Assumptions

- The API does not publish disk sizes. `disk_size_gib` comes from Microsoft's
  disk size table (https://learn.microsoft.com/en-us/azure/virtual-machines/disks-types).
  A tier that is not in the table gets no size.
- Meters ending in `- Free` have price 0 and region `Global`. Microsoft does
  not document them. They seem to back the Azure free account (two 64 GB P6
  disks, 1 GB of snapshots and 2 million I/O operations for 12 months), but the
  API also has free meters for P4, E4 and E6 that are not in that list. They
  are kept and marked `offer: free`.
- Microsoft says "primary meters are used for charges and billing". Items with
  `isPrimaryMeterRegion: false` are still kept, with the flag stored in
  `Attributes["is_primary_meter_region"]`, so nothing is dropped. A consumer
  that wants only billable prices should filter on `true`. For example, the
  old `S10 LRS Disk Operations` meter (0.0005, non-primary) is listed next to
  the current `Disk Transactions` meter (0.00065, primary).

## What cannot be represented

- Premium SSD v2 and Ultra Disk cost is the sum of several records (capacity,
  IOPS, throughput, and vCPU for Ultra, including the free first tiers). The
  schema cannot say that records belong together, so the consumer has to add
  them. A possible schema addition would be a "component of" or bundle field.
- Performance tiers (running a Premium SSD at a higher P tier) are charged at
  that tier's normal price. No extra records are needed, but the rules are not
  modelled: only Premium SSD, never below the disk's own tier, and P60 to P80
  only for disks larger than 4 TiB.

## Known limitations

- Fixed-size disks show a monthly price, but Azure bills them per hour, and a
  disk is billed at the next tier up (a 200 GiB Standard SSD is billed as E15).
  The catalog stores the published monthly price.
- Only Premium SSD P30 to P80 have reservations. The API does not list upfront
  and monthly payment options as different prices.
- Standard transactions have an hourly cap in billing. The catalog stores the
  per-10,000 price only.
- Public retail prices only. EA, CSP or negotiated prices are not included.
- The API changes over time, so the tests use saved fixtures.

## Fixtures

- `testdata/retail_prices.json`: 39 real items from the API, unchanged, in the
  API page format. They are from westeurope, eastus and `Global`. They cover all
  five products, LRS and ZRS, 1-year and 10-year reservations, Premium SSD v2
  tiers, Ultra meters, snapshots, bursting, encryption, a non-primary meter,
  free meters (one shared by two SKUs) and one out-of-scope Premium Page Blob item.
- `testdata/retail_prices_eur.json`: the same P30 prices in EUR.

## Running the tests

```bash
go test ./connectors/azure/managed-disks/ -v
make check
```

The tests cover the connector contract (USD and EUR), full checks of single
records, reservations, tiers, all meter types, free meters, input order,
unit/term/meter parsing, invalid items (rejected by the connector or by the
pipeline), page decoding, and the live path (pagination, retry on 429/5xx,
other HTTP errors, a response without `Items`, and cancellation).
