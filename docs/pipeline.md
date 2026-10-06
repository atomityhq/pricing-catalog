# Catalog Build Pipeline

The candidate-facing connector contract deliberately ends before catalog
persistence and release operations. The repository still defines the canonical
boundary used by the eventual refresh system.

## Current skeleton

```text
connector.Fetch
      ↓
canonical PricingRecord values
      ↓
internal/pipeline.BuildSnapshot
      ↓
validation
      ↓
deterministic ordering
      ↓
Snapshot
```

A connector must return canonical records. It must not write the catalog itself.

`BuildSnapshot` rejects an empty connector result and invalid records, and assigns
snapshot-level metadata such as `version` and `generated_at`.

## Future maintainer pipeline

```text
scheduled refresh
      ↓
connector fetch + normalize
      ↓
validate
      ↓
snapshot
      ↓
compare with current snapshot
      ↓
human-readable change report
      ↓
maintainer review
      ↓
release new catalog version
```

The future stages are intentionally not implemented here yet. The important point
is that connectors already terminate at the same snapshot boundary they will use.
