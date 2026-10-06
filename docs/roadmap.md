# Roadmap

The candidate-facing skeleton intentionally stops short of the full production system.

## Skeleton completed

- Canonical pricing model
- Deterministic decimal price representation
- Connector interface
- Connector validation/testkit
- Connector → validation → snapshot pipeline boundary
- Simple reference connector
- Complex reference connector
- Fixture-based tests
- Snapshot validation/loading
- Basic query API
- Candidate assignment and contributor guidance

## Future production work

- Provider registry/discovery
- HTTP/API client conventions
- Scheduled provider refreshes
- Human-readable pricing diffs
- Safe refresh/retry behavior
- Release automation
- Historical snapshot retention strategy
- Catalog size/performance measurement
- Public package/module path
- Maintainer-only evaluation automation

Candidates contribute connectors; maintainers own the refresh, comparison,
publication, and release lifecycle around those connectors.

The candidate contract should remain stable while these capabilities are added.
