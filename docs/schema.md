# Canonical Pricing Schema

The canonical model is in `pkg/catalog/model.go`.

## PricingRecord

A `PricingRecord` represents one distinct pricing combination.

Important dimensions include:

| Field | Meaning |
|---|---|
| `Provider` | Normalized provider identifier |
| `Product` | Human-readable canonical product name |
| `SKU` | Canonical/provider-facing SKU identifier |
| `ProviderProductID` | Provider's product identifier |
| `ProviderSKUID` | Provider's SKU identifier |
| `Region` | Normalized pricing region |
| `PurchaseModel` | e.g. on-demand, committed |
| `BillingUnit` | e.g. hour, GB-month |
| `PricingDimension` | Main unit/dimension being priced |
| `Dimensions` | Additional meaningful dimensions |
| `Price` | Exact decimal price + currency |
| `Tier` | Optional quantity tier |
| `EffectiveFrom` / `EffectiveTo` | Optional validity window |
| `Source` | Data provenance |
| `Attributes` | Provider metadata that should be retained but is not yet first-class |

## Price representation

Prices use decimal strings, not `float64`.

Example:

```json
{
  "amount": "0.0850",
  "currency": "EUR"
}
```

This avoids introducing floating-point rounding into pricing data.

## Information preservation

The generic schema should normalize representation, not destroy meaning.

For example, these should remain distinguishable:

```text
SKU X / eu-west / on_demand / hour / 0.08
SKU X / eu-central / on_demand / hour / 0.085
SKU X / eu-west / committed / hour / 0.06
```

Tiered pricing should remain tiered rather than being collapsed into an arbitrary average or range.

## Provenance

Every record requires a source type, source URL, and retrieval timestamp.

Consumers do not need to fetch the source at runtime, but maintainers must be able to understand where catalog values came from.
