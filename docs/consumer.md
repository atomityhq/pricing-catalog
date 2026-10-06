# Consumer Guide

Private Atomity repositories should consume a released catalog version rather than calling provider pricing APIs directly.

Conceptually:

```text
private service
      ↓
pricing-catalog vX.Y.Z
      ↓
embedded/versioned catalog
      ↓
query canonical records
```

A pinned version makes the pricing inputs reproducible. Updating the catalog should therefore be an intentional dependency upgrade.

## Querying

Use the generic query API:

```go
prices := c.Prices(catalog.PriceQuery{
    Provider:      "provider",
    Product:       "product",
    Region:        "eu-west",
    PurchaseModel: "on_demand",
})
```

Consumers should not need provider-specific parsing logic.

## No runtime provider calls

The library does not require a provider network call when a consumer asks for pricing. Provider refresh happens outside the consumer process and results in a new catalog version.


Consumers should not implement their own provider refresh logic. The refresh pipeline
is a maintainer concern and will update the catalog before a new library version is
published.
