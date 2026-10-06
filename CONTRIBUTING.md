# Contributing

Thanks for contributing to `pricing-catalog`.

## General expectations

- Keep provider-specific behavior inside the provider connector.
- Normalize data into the canonical model instead of adding provider-specific fields to the generic API unless the distinction is genuinely universal.
- Preserve commercially meaningful pricing dimensions.
- Prefer authoritative provider sources over rendered website scraping when a structured source exists.
- Do not use floating-point numbers for catalog prices.
- Add deterministic fixtures for non-trivial provider data.
- Add regression tests for every parsing or normalization edge case you fix.
- Document important assumptions and known limitations.

## Local checks

```bash
make check
make validate-example
```

## Connector structure

A connector will normally contain:

```text
connectors/<provider>/
├── connector.go
├── parser.go            # optional
├── normalizer.go        # optional
├── connector_test.go
└── testdata/
```

The split between files is up to the contributor. Keep the architecture understandable rather than creating abstractions for their own sake.

## Pull requests

The pull request should explain:

1. which source was used;
2. how the provider pricing model works;
3. how it maps to the canonical schema;
4. what information is intentionally not represented;
5. what tests and fixtures were added;
6. any known limitations.
