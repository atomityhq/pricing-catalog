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

## Refresh pipeline boundary

The connector-to-snapshot portion of the refresh pipeline is part of the skeleton
now. This is deliberate: candidate connectors must plug into the same pipeline
that the eventual production refresh system will use.

```text
connector fetch + normalize
      ↓
generic validation
      ↓
deterministic ordering
      ↓
catalog snapshot
```

`Connector.Fetch` is the provider-specific fetch + normalization boundary.
`internal/pipeline.BuildSnapshot` owns generic validation, deterministic ordering,
and snapshot construction.

Candidates must not:

- write catalog snapshots directly;
- create provider-specific persistence formats;
- create provider-specific refresh mechanisms;
- bypass `catalog.ValidateRecords`;
- change the canonical schema merely to fit one provider.

The following pieces remain maintainer-owned future work:

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

The skeleton intentionally stops before scheduling, release automation, and
provider refresh orchestration. Those components consume the snapshot pipeline;
candidates do not implement them.


See [`pipeline.md`](pipeline.md) for the current connector-to-snapshot contract and the future maintainer lifecycle.
