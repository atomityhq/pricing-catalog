# Amazon EBS connector design

## Source and scope

The connector consumes an `AmazonEC2` JSON offer file from the public AWS Price
List Bulk API. EBS products are published within this offer. The authoritative
references are the [AWS offer-file structure](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/reading-service-price-list-file-for-services.html),
the [EBS usage codes](https://docs.aws.amazon.com/ebs/latest/userguide/ebs-billing-usage-reports.html),
and the [Amazon EBS pricing page](https://aws.amazon.com/ebs/pricing/).

Products are selected by `attributes.usagetype`: either `EBS:` at the start or
`-EBS:` after a region prefix. Other AmazonEC2 products are skipped. This covers
EBS volume storage, provisioned IOPS and throughput, snapshot storage and related
EBS charges, including fast snapshot restore. The connector preserves the source
meter rather than combining these charges into one estimated volume price.

Only On-Demand terms and USD prices are supported. A different purchase model
attached to an EBS SKU causes an error, even if On-Demand terms also exist.
Unsupported currencies also cause an error instead of silently dropping prices.
Terms for unrelated EC2 products do not affect the EBS mapping.

## Fetching and normalization

`New(io.Reader)` retains the original constructor and the existing
`pricing-catalog/pkg/catalog` import. A supplied reader allows the caller to use
a downloaded file or an in-memory fixture. A reader is consumable; callers must
provide a fresh reader for each fetch.

`New(nil)` uses HTTP GET with `Accept: application/json`. The default source is
the current AmazonEC2 offer for `us-east-1`, replacing the original placeholder
URL. The HTTP client has a 30-second timeout and accepts only HTTP 200. The
response body is closed on success and failure. Both reader and HTTP inputs are
limited to 256 MiB; the HTTP path also checks the declared `Content-Length`.

`connector.go` handles transport and input limits. `normalize.go` decodes and
validates the offer, selects EBS products, and builds canonical records. This
keeps the mapping testable without an HTTP request. The source structs describe
the fields used by this connector rather than the entire AWS schema.

The output uses the existing [canonical model](https://github.com/atomityhq/pricing-catalog/blob/130736c5fb2a1e08c5818689fafa2fdfef3b454f/pkg/catalog/model.go).
No replacement catalog package, module file, consumer API, or provider-specific
snapshot pipeline is added. The repository's generic connector contract test
exercises normalization, canonical validation, and snapshot determinism together.

## Mapping decisions

One record is emitted for each supported EBS price dimension.

| Canonical field | Mapping |
| --- | --- |
| `Provider`, `Product` | `aws`, `Amazon EBS` |
| `SKU`, `ProviderSKUID` | Original AWS product SKU |
| `ProviderProductID` | `AmazonEC2` |
| `Region` | Required `attributes.regionCode`, including Local Zone codes |
| `PurchaseModel` | `on_demand` |
| `BillingUnit` | Source `unit`, unchanged |
| `PricingDimension` | Source `attributes.usagetype`, unchanged |
| `Price` | Original USD decimal string; no floating-point conversion |
| `Tier` | `beginRange` and `endRange`; `Inf` becomes an empty upper bound |
| `EffectiveFrom` | Required RFC3339 `effectiveDate`, converted to UTC |
| `Source` | Type `aws-price-list-json` and a versioned AWS offer URL |

Billing units are required to be non-empty but are not restricted to `Hrs`.
The fixture includes `GB-Mo`, `IOPS-Mo`, `MBps-Mo`, and `DSU-Hrs`. Keeping the
source spelling avoids changing the scale or meaning of a published price.

`Dimensions` retains the rate code, tier boundaries, unit, and available usage,
volume, operation, location, and location-type attributes. Term attributes use
the `term_attribute.` prefix so they cannot overwrite core dimension fields.
Non-empty `appliesTo` lists are preserved as JSON in `Dimensions["applies_to"]`.

`Attributes` retains all source product attributes, plus the price description,
product family, and offer version. The source URL is constructed from the offer
version and product region; it identifies the corresponding AWS publication,
even when the input was read from a local file. It is not the local file path
or the address of a test server.

Product SKUs, purchase-model names, term keys, rate keys, and currency keys are
sorted before iteration. Records own their maps and effective-date values, so
changing one record cannot mutate another record.

## Invalid and incomplete input

The connector returns an error and no partial records for:

- Malformed or truncated JSON, trailing data, or incompatible JSON field types.
- A missing or unsupported `formatVersion` or `offerCode`, a blank offer version,
  an empty product collection, or missing `terms.OnDemand`.
- A missing or inconsistent product SKU, or a missing `usagetype`. These fields
  are checked before filtering so an incomplete EBS product is not silently lost.
- An EBS product without a product family, AmazonEC2 service code, region, or
  matching non-empty On-Demand terms.
- A term with an inconsistent SKU or term key, a missing term code, an invalid
  effective date, or no price dimensions.
- A price dimension without a matching rate code, description, billing unit,
  valid tier boundaries, or a non-empty price map.
- An unsupported currency or an invalid, negative, or empty price amount.

Quantities and amounts must be non-negative decimal strings. Scientific
notation, fractions, signs, whitespace, `NaN`, and numeric JSON values are
rejected. An unbounded tier still requires a valid start quantity. Finite upper
bounds must be greater than their lower bounds; comparison uses `big.Rat` to
avoid rounding large or precise decimal quantities.

Unknown source fields are ignored to allow AWS metadata outside the mapping.
`termAttributes`, `appliesTo`, and unused product attributes are optional. A valid
offer containing no selected EBS products produces an empty result.

## Tests and fixture

`testdata/offer.json` is a small synthetic offer following the AWS structure.
Its SKUs, publication version, and prices are illustrative test data, not a
downloaded AWS price publication. It contains volume capacity, three IOPS tiers,
throughput, standard and archive snapshot storage, fast snapshot restore, a Local
Zone, and an unrelated EC2 product.

`normalize_test.go` checks the canonical mapping, units, regions, prices, tier
boundaries, UTC dates, metadata, optional fields, deterministic ordering, and
independent record values. Negative tests cover missing and null fields, wrong
types, broken JSON, invalid values, unsupported purchase models and currencies,
and invalid ranges. Zero prices and long decimal prices remain unchanged.

`connector_test.go` checks the generic connector contract, reader input, HTTP
method and headers, response errors, broken HTTP response JSON, read and transport
failures, response-body closing, size-limit boundaries, and context cancellation.
HTTP tests use `httptest` or a local transport; no test depends on live AWS prices.

Run from the complete repository root:

```bash
go test ./connectors/aws/ebs -cover
go test -race ./connectors/aws/ebs
make check
make validate-example
```

The local working folder contains only the connector. Validation uses a temporary
copy of the public repository at commit
`130736c5fb2a1e08c5818689fafa2fdfef3b454f`, with these connector files added.
The existing import and shared packages are kept unchanged.

Validation passed: connector tests with 99.3% statement coverage, the connector
race check, `make check` (formatting, vet, and all repository tests), and
`make validate-example`.

## Limitations

The entire input is buffered and decoded in memory. Regional offer files are
preferred, and files above 256 MiB are rejected. The default HTTP source covers
only `us-east-1`; other regions can be supplied through a reader. HTTP requests
honor context cancellation. An arbitrary blocking reader cannot be interrupted
by the connector; its context is checked before and after reading.

This is focused validation, not a complete AWS schema validator. Unknown fields
and duplicate JSON object keys follow Go's `encoding/json` behavior. Non-empty
units are preserved without checking their semantic compatibility with each
usage code. Non-EBS pricing is not normalized.

The connector preserves conditional `appliesTo` metadata but does not evaluate
eligibility, aggregate usage, calculate bills, convert units or currencies,
apply taxes or discounts, or derive effective end dates. These operations are
outside the provider-to-catalog mapping.

## AI assistance

AI (Codex) was used to help write and refine the tests and to speed up writing
and correcting the normalization code in `normalize.go`. It also assisted with
formatting and drafting this document. The mapping is checked against the
existing canonical model, fixture expectations, and connector contract tests.
