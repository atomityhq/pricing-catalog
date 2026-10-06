# pricing-catalog

Open-source cloud pricing catalog and Go library for Atomity.

`pricing-catalog` has two jobs:

1. **Connect to public cloud-provider pricing/catalog sources**, normalize their data, and keep the canonical catalog up to date.
2. **Expose versioned, deterministic pricing data as a Go library** to private Atomity services.

The repository also serves as the codebase for a production-style hiring exercise: candidates can add a new provider connector and submit it as a pull request.

## Design at a glance

```text
Cloud provider pricing source
            │
            ▼
       Provider connector
            │
            ▼
    Provider-specific models
            │
            ▼
        Normalization
            │
            ▼
     Canonical pricing model
            │
       ┌────┴─────┐
       │          │
       ▼          ▼
   Validation   Snapshot
       │          │
       └────┬─────┘
            ▼
     Versioned catalog
            │
            ▼
     pricing-catalog Go API
            │
            ▼
   Atomity private repositories
```

Provider pricing is expected to change over time. Consumers should depend on a **pinned library version**, while catalog refreshes are handled by maintainers and released as new versions.

## Repository layout

```text
pkg/catalog/                Canonical model, validation, serialization, queries
pkg/connector/              Connector contract and reusable test helpers
connectors/                  Provider connectors and reference examples
cmd/pricing-catalog/         Small maintenance/validation CLI
tests/                       Cross-connector tests
docs/                        Architecture and contributor documentation
internal/pipeline/           Canonical connector → validation → snapshot pipeline
internal/evaluation/         Maintainer-only evaluation entry point
.github/workflows/           Public CI
```

## Quick start

```bash
make check
make validate-example
```

Run the full test suite directly:

```bash
go test ./...
```

## Consumer example

```go
package main

import (
    "os"

    "pricing-catalog/pkg/catalog"
)

func main() {
    file, _ := os.Open("catalog.json")
    snapshot, _ := catalog.LoadJSON(file)
    c, _ := catalog.FromSnapshot(snapshot)

    prices := c.Prices(catalog.PriceQuery{
        Provider: "example-cloud",
        Region:   "eu-west",
    })

    _ = prices
}
```

The public API is intentionally small. Consumers should query canonical data without knowing how any provider exposes its pricing.

## Contributor and candidate entry points

Start with:

- [`docs/architecture.md`](docs/architecture.md)
- [`docs/pipeline.md`](docs/pipeline.md)
- [`docs/schema.md`](docs/schema.md)
- [`docs/connectors.md`](docs/connectors.md)
- [`docs/consumer.md`](docs/consumer.md)
- [`ASSIGNMENT.md`](ASSIGNMENT.md)
- [`CONTRIBUTING.md`](CONTRIBUTING.md)

Reference implementations:

- [`connectors/example-simple`](connectors/example-simple)
- [`connectors/example-complex`](connectors/example-complex)

## Status

This repository currently provides the **library and candidate-assignment skeleton**,
including the connector → validation → snapshot pipeline boundary.

The remaining production lifecycle is intentionally outside the candidate scope:

```text
scheduled refresh
      ↓
compare with current snapshot
      ↓
human-readable change report
      ↓
maintainer review
      ↓
release new catalog version
```

These capabilities can be added later without changing the candidate-facing
connector contract.
