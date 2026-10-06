# Architecture

## Goals

`pricing-catalog` is designed around two separate concerns:

1. ingesting provider pricing data;
2. serving normalized, deterministic catalog data to consumers.

The normal runtime path for a consumer should **not** call a provider API.

```text
Provider source
      ↓
Connector
      ↓
Raw provider model
      ↓
Normalizer
      ↓
Canonical records
      ↓
Validation
      ↓
Snapshot / release
      ↓
Go library
      ↓
Consumer
```

## Connector boundary

Provider-specific concerns belong in the connector:

- HTTP/API calls
- authentication to public endpoints when required
- provider payload parsing
- provider-specific terminology
- provider-specific transformations
- source-specific quirks

The generic catalog package should not know these details.

## Canonical catalog

The canonical catalog is the stable boundary between provider integrations and consumers.

Adding a new provider should normally require adding a connector, not changing the consumer API.

## Determinism

A released catalog should be deterministic for its version.

If provider prices change, the update process should create a new catalog version rather than silently changing data inside an already-pinned dependency.

## Future refresh pipeline

The repository is intended to grow toward:

```text
scheduled refresh
      ↓
connector fetch
      ↓
normalize
      ↓
validate
      ↓
compare with current snapshot
      ↓
human-readable change report
      ↓
maintainer review
      ↓
release new catalog version
```

The complete automation is intentionally outside the candidate skeleton for now.
