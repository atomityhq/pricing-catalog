# Example complex connector design

This example intentionally uses synthetic provider data.

## Source

The connector reads a fixture shaped like a public provider catalog. A real connector would fetch an authoritative provider API or catalog export.

## Pricing model

The synthetic provider distinguishes:

- product;
- SKU;
- region;
- purchase model;
- billing unit;
- resource dimensions;
- quantity tiers;
- currency.

## Mapping decisions

Provider identifiers are retained in `ProviderProductID` and `ProviderSKUID`.

Resource characteristics such as vCPU and memory are retained in `Dimensions` because they affect SKU identity but are not universal pricing fields.

Each tier becomes a separate canonical record so its price cannot be confused with another tier.

On-demand and committed pricing become separate records even when the SKU and region are identical.

## Limitations

This example does not demonstrate every possible provider pricing model. In particular, it does not model discounts, one-time fees, usage aggregations, or multiple currencies for the same price dimension.
