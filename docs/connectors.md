# Connector Guide

## Interface

Every connector implements:

```go
type Connector interface {
    Name() string
    Fetch(ctx context.Context) ([]catalog.PricingRecord, error)
}
```

The interface is intentionally small.

A connector can internally contain whatever provider-specific types and helpers it needs.

## Pipeline contract

The connector is responsible for provider-specific work:

```text
provider source
      ↓
connector
      ↓
provider-specific parsing
      ↓
normalization
      ↓
canonical PricingRecord values
```

The generic pipeline then performs:

```text
canonical records
      ↓
validation
      ↓
deterministic ordering
      ↓
catalog snapshot
```

Candidates must not implement their own snapshot generation, persistence, or
refresh pipeline.

The reusable test helper `connector/testkit.AssertConnectorContract` runs the
connector through this same pipeline.

## Recommended flow

```text
provider source
      ↓
provider models
      ↓
parse / clean
      ↓
normalize
      ↓
canonical PricingRecord
      ↓
ValidateRecords
```

## Reference connectors

### `example-simple`

Demonstrates a basic one-price-per-SKU/region mapping.

Use it to understand:

- source decoding;
- canonical field mapping;
- fixtures;
- basic contract tests.

### `example-complex`

Demonstrates:

- multiple purchase models;
- multiple dimensions;
- tiered pricing;
- exact decimal prices;
- preserving provider detail.

This is the more important example for the candidate assignment.

## Directory convention

All production provider/service connectors MUST use:

    connectors/<provider>/<service>/

Examples:

    connectors/aws/ec2/
    connectors/aws/s3/
    connectors/gcp/compute/
    connectors/azure/virtual-machines/

Directory names MUST:

- use lowercase characters;
- use hyphens for word separators;
- identify the provider at the first level;
- identify the provider pricing service/domain at the second level.

Provider-only connector directories are not permitted. For example:

    connectors/aws/

must not contain a connector implementation directly.

The repository does not contain empty directories for future connectors.
The directory is created when the corresponding connector is implemented.

The following are intentional exceptions:

    connectors/example-simple/
    connectors/example-complex/

These are framework examples rather than provider/service connectors.

## Network access

The example connectors use fixtures so the default test suite is deterministic.

Real connectors may use an HTTP client, but tests should normally use captured/sanitized fixtures rather than depend on the provider being online.

## Error handling

Provider fetch and parsing failures should be returned rather than silently producing an empty or partial catalog.

The future update pipeline should treat validation/fetch failures as a failed refresh and keep the last known-good snapshot.
