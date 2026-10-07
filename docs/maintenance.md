# Maintenance and Pricing Updates

Cloud provider pricing changes. The catalog therefore needs a refresh mechanism, but consumers should still see deterministic data for a pinned library version.

## Intended model

```text
provider changes price
        ↓
scheduled refresh
        ↓
connector fetches new data
        ↓
normalize + validate
        ↓
compare to current snapshot
        ↓
generate human-readable change report
        ↓
maintainer review
        ↓
merge
        ↓
new catalog version
```

The refresh workflow should use the same connector and normalization boundaries as the rest of the library. Provider-specific refresh logic should remain inside the relevant connector rather than leaking into the catalog or consumer API.

## Safe failure behavior

A provider refresh must never replace a known-good catalog with an empty, incomplete, or invalid result.

Expected behavior:

```text
refresh fails
     ↓
validation/update fails
     ↓
do not publish new snapshot
     ↓
keep last known-good snapshot
     ↓
notify maintainer
```

A successful provider fetch is therefore not sufficient by itself. The candidate snapshot must pass normalization and validation before it can replace the current catalog.

## Reviewability

As the catalog grows, generated-data diffs can become difficult to review. The update tooling should generate a concise human-readable change report, for example:

```text
Provider: example-cloud
SKU: vm-general-4
Region: eu-west
Purchase model: on_demand
0.1200 EUR/hour → 0.1250 EUR/hour
```

The report should make it obvious what changed before a maintainer publishes a new version.

The update process should make it possible to distinguish:

- added pricing records
- removed pricing records
- changed prices
- changed regions
- changed purchase models
- changed billing units or pricing dimensions
- other commercially meaningful changes

## Historical data

The current catalog model carries optional effective dates, and Git history plus versioned releases provide a baseline for reproducibility.

If consumers later require point-in-time price lookup, a dedicated historical snapshot model can be introduced without changing the connector boundary.

The initial implementation should therefore prioritize deterministic versioned snapshots rather than introducing a separate historical pricing system prematurely.