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
maintainer review
        ↓
merge
        ↓
new catalog version
```

## Safe failure behavior

A provider refresh must never replace a known-good catalog with an empty or invalid result.

Expected behavior:

```text
refresh fails
     ↓
validation/update fails
     ↓
keep last known-good snapshot
     ↓
notify maintainer
```

## Reviewability

As the catalog grows, generated-data diffs can become difficult to review. The long-term update tooling should generate a concise human-readable change report, for example:

```text
Provider: example-cloud
SKU: vm-general-4
Region: eu-west
Purchase model: on_demand
0.1200 EUR/hour → 0.1250 EUR/hour
```

The report should make it obvious what changed before a maintainer publishes a new version.

## Historical data

The current skeleton carries optional effective dates and full Git history/versioning can provide a baseline for reproducibility.

If consumers later require point-in-time price lookup, a dedicated historical snapshot model can be introduced without changing the connector boundary.
