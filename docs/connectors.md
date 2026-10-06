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

## Network access

The example connectors use fixtures so the default test suite is deterministic.

Real connectors may use an HTTP client, but tests should normally use captured/sanitized fixtures rather than depend on the provider being online.

## Error handling

Provider fetch and parsing failures should be returned rather than silently producing an empty or partial catalog.

The future update pipeline should treat validation/fetch failures as a failed refresh and keep the last known-good snapshot.
