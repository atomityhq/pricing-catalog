# Google Persistent Disk connector design

## Source

The authoritative public pricing source for Google Persistent Disk is the
**Google Cloud Billing Catalog API v1**, the same source the Compute Engine
connector in this repository uses:

- **Service Identifier:** `services/6F81-5844-456A` (Compute Engine)
- **REST Endpoint:** `https://cloudbilling.googleapis.com/v1/services/6F81-5844-456A/skus`

Persistent Disk has no dedicated service in the catalog: its SKUs live in
the Compute Engine service alongside VM, license, networking, Hyperdisk and
Local SSD SKUs. In production refreshes, the connector fetches paginated
SKU responses (`nextPageToken`) over HTTPS using `http.Client`. To keep unit
and contract tests deterministic and free of private credentials, the
connector supports reading captured JSON payloads via `io.Reader`
(`WithReader`) and local test servers (`WithBaseURL`) backed by
`testdata/source.json`.

Fetching the live endpoint requires a Google Cloud API key
(`GCP_API_KEY` environment variable, sent as the `key` query parameter);
without a key the API answers HTTP 403. The test suite never depends on
the live API.

Prices in the fixture were cross-checked against the official pricing page
**https://cloud.google.com/compute/disks-image-pricing** (read
2026-11-24, region `us-central1`): the page quotes hourly rates and gives
worked examples (Standard 200 GB = $8.00/month, SSD 200 GB =
$34.00/month), which match the monthly rates in the fixture via the
730-hours-per-month identity (hourly rate x 730 = monthly rate).

## Pricing model

Persistent Disk is durable block storage attached to Compute Engine VMs.
Its pricing has four dimensions:

1. **Disk type (`PricingDimension`):** Standard, Balanced, SSD and Extreme
   persistent disks are billed per GiB-month of provisioned capacity
   (`GiBy.mo`). Extreme disks additionally charge for provisioned IOPS.
   IOPS are included in the capacity price for SSD and Balanced disks.
2. **Replication (`Dimensions["replication"]`):** zonal disks live in one
   zone; regional disks are replicated across two zones in the same region
   and cost twice the zonal rate for the same disk type.
3. **Snapshots (`Product`):** snapshot storage is a separate product from
   disks. Standard snapshots cost $0.05/GiB-month, archive snapshots
   $0.019/GiB-month plus a $0.019/GiB retrieval charge, and instant
   snapshots charge $0.05 per creation operation plus storage at the
   source disk type's rate. Multi-regional snapshots are priced per
   multi-region (`us`, `eu`, `asia`) rather than per region.
4. **Tiers (`Tier`):** the first 30 GiB-months of Standard disk per
   account per month are free; beyond that the Standard rate applies.
   This is modelled as a two-bracket tiered rate (0-30 at $0.00, 30+ at
   $0.04) — see Assumptions.

**Purchase model:** the public pricing page lists no committed-use or
spot pricing for Persistent Disk, so every record is `on_demand`. The
connector still maps `commit*` and `preemptible` usage types the same way
the Compute Engine connector does, defensively, in case the API returns
them.

## Mapping decisions

- **Scope filtering:** the Compute Engine catalog contains many unrelated
  SKUs, so the normalizer keeps only the Storage family and then applies
  description rules (see `isSupportedScope` in `normalize.go`):
  Hyperdisk, Local SSD and storage-pool SKUs are excluded by resource
  group (`LocalSSD`, `HDBSP`) and by description ("hyperdisk",
  "local ssd", "storage pool"); a SKU is kept only when its description
  mentions Persistent Disk ("persistent disk" or a standalone "pd" word,
  e.g. "Storage PD Capacity") or snapshots. The rule is deliberately
  narrow: an unexpected description is dropped rather than silently
  misclassified, and `normalize` fails loudly when zero records are
  produced.
- **Provider & Product identity:** `Provider` is normalized to `"gcp"`.
  `ProviderProductID` retains `"services/6F81-5844-456A"`, and
  `ProviderSKUID` / `SKU` retain the upstream `skuId`. `Product` is
  `"Persistent Disk"` for disks and `"Persistent Disk Snapshot"` for
  snapshots — snapshots are a separate product because their billing
  unit, lifecycle and price differ from disk capacity.
- **Pricing dimensions:** the disk type (`standard` / `balanced` / `ssd` /
  `extreme`) or the snapshot kind (`standard_snapshot` /
  `archive_snapshot` / `instant_snapshot`) is the `PricingDimension`.
  `Dimensions` preserves `replication` (`zonal` / `regional` /
  `multi_regional`), `disk_type` (`pd-standard`, `pd-balanced`, `pd-ssd`,
  `pd-extreme`), `snapshot_type` (`standard` / `archive` / `instant`),
  `iops=provisioned` for Extreme IOPS charges and `charge=retrieval` for
  archive snapshot retrieval.
- **Region unrolling:** a single GCP SKU frequently lists multiple regions
  in `serviceRegions` (or `geoTaxonomy.regions`). The normalizer unrolls
  each unique region into a distinct `PricingRecord`. Multi-regional
  snapshots keep their multi-region code (`us`, `eu`, `asia`) as the
  region instead of being split into individual regions. The
  `replication` dimension is `zonal` for zonal disks and instant
  snapshots (instant snapshots live in the same zone as their source
  disk), `regional` for regional disks and standard/archive snapshots,
  and `multi_regional` for multi-regional snapshots.
- **Exact decimal conversion:** GCP represents unit prices as integer
  `units` (string) and `nanos` (`int32`, 10^-9). The connector formats
  these components directly into exact decimal strings without `float64`
  arithmetic (same logic as the Compute Engine connector).
- **Tier interval construction:** GCP specifies only the lower bound
  (`startUsageAmount`) for each element in `tieredRates`. The connector
  maps tier `i`'s `StartQuantity` from `rates[i].startUsageAmount` and
  populates `EndQuantity` from `rates[i+1].startUsageAmount` (leaving
  `EndQuantity` empty for the final open-ended tier).
- **Billing units:** `GiBy.mo` becomes `GiB-month`, `mo` becomes
  `month`, `GiBy.h` becomes `GiB-hour`; the instant-snapshot creation
  fee keeps `operation` and the archive retrieval fee keeps `GiB`.
  Unknown units are preserved verbatim rather than discarded.
- **Provenance and metadata:** raw provider fields
  (`service_provider_name`, `resource_family`, `resource_group`,
  `usage_type`, `geo_taxonomy_type`, `base_unit`,
  `aggregation_interval`, `aggregation_level`) are retained in
  `Attributes`, while `EffectiveFrom` preserves
  `pricingInfo.effectiveTime` in UTC.
- **Deterministic output:** all normalized records are sorted by
  `catalog.RecordKey` prior to returning from `Fetch`.

## Assumptions

- **Fixture data is hand-written test data.** `testdata/source.json`
  follows the Cloud Billing Catalog API response shape, and its prices
  were cross-checked against the official pricing page (see Source), but
  the SKU identifiers and the `resourceGroup` values (`PD`, `Snapshot`)
  are test conventions, not data copied from a live API response. The
  live catalog could not be fetched while writing this connector because
  it requires a GCP API key.
- **Scope filter relies on description wording.** The live API's
  `resourceGroup` and `description` values for classic Persistent Disk
  SKUs could not be verified (the only Storage SKUs visible in this
  repository's fixtures are Hyperdisk). The filter therefore keys on
  `resourceFamily == "Storage"` plus description markers and explicit
  Hyperdisk/Local SSD exclusions, and is documented as the first thing
  to re-verify against a live SKU list. If Google changes description
  wording, in-scope SKUs could be missed; the connector fails loudly
  (zero records is an error) rather than returning a partial catalog.
- **Free tier as a tiered rate.** The documented 30 GiB-months free
  Standard disk per account per month is modelled as a two-bracket
  tiered rate (0-30 at $0.00, 30+ at the Standard rate). Whether the
  live API expresses the free tier this way has not been verified.
- **Extreme IOPS billing unit.** The $0.065/month provisioned-IOPS
  charge is modelled with usage unit `mo` (billing unit `month`),
  matching how the Hyperdisk IOPS SKU in the Compute Engine fixture
  expresses its IOPS charge. The exact API unit for Extreme IOPS has
  not been verified against a live SKU.
- **Snapshots share the Compute Engine service.** Snapshot SKUs are
  assumed to live in `services/6F81-5844-456A` (the pricing page lists
  snapshots under Compute Engine). If they turn out to belong to a
  different service, the connector would need a second service ID.
- **All records are on_demand.** No committed-use or spot Persistent
  Disk pricing was found on the public pricing page.

## Information that could not be represented

- **Per-account free tier.** The 30 GiB free tier applies per account,
  not per SKU; the canonical model can only express it as a quantity
  tier on the Standard disk SKU, which is an approximation of the
  account-level rule.
- **Sustained Use Discounts / spend-based CUDs:** automatic monthly
  usage-duration discounts and billing-account-level spend commitments
  depend on dynamic account-wide consumption rather than static SKU
  unit rates, and therefore cannot be represented as static catalog
  SKU records (same limitation as the Compute Engine connector).
- **Network charges for cross-region snapshot operations** (creating or
  restoring a snapshot in a different region, multi-regional
  download/upload) belong to the Network family and are outside this
  task's "disks and snapshots" scope; they are intentionally excluded.
- **Regional vs zonal snapshot storage costs** are preserved through the
  `replication` dimension; the snapshot recycle-bin charge is not
  modelled as a separate SKU because it is unclear from public sources
  whether it is a distinct SKU or part of the standard snapshot SKU.

## Out of scope (intentionally excluded)

Hyperdisk (all types), Local SSD, async replication, network charges,
custom image storage, premium OS images, and snapshot recycle-bin
charges (see above).

## How to run the connector tests

```bash
go test ./connectors/gcp/persistent-disk/ -v
go test ./connectors/gcp/persistent-disk/ -run TestConnectorContract -v
make check   # vet + full test suite + gofmt, must pass before opening a PR
```

To fetch live data instead of the fixture, set `GCP_API_KEY` and call
`New()` without `WithReader`; the connector pages through
`https://cloudbilling.googleapis.com/v1/services/6F81-5844-456A/skus`.

## Development notes

This connector was developed with AI assistance (code, fixtures and this
document were drafted with an AI coding assistant and reviewed against
the repository's conventions and the sources cited above); that
assistance should also be disclosed in the pull request description.
