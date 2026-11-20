# Hetzner Cloud connector design

## Source

Authoritative structured source: **Hetzner Cloud API** `GET /v1/pricing`

- Endpoint: `https://api.hetzner.cloud/v1/pricing`
- Docs: https://docs.hetzner.cloud/reference/cloud#pricing
- Public marketing confirmation of list prices / traffic / volumes: https://www.hetzner.com/cloud

The API returns currency, VAT rate, and net/gross prices for server types (per location), volumes, and traffic overage (as `price_per_tb_traffic` on each server-type location entry). The top-level legacy `traffic` field is deprecated/removed; traffic is represented per server type and location.

Live fetches may send `Authorization: Bearer <token>` when `HCLOUD_TOKEN` (or `WithToken`) is set, because the endpoint is project-scoped (currency/VAT follow the project owner). Unit and contract tests use `testdata/price.json` via `WithReader` and never require credentials.

## Test fixture

The test fixture (`testdata/price.json`) is a curated synthetic representation based on Hetzner's published Cloud pricing information. It is used for deterministic offline tests and is not claimed to be a captured response from the live pricing API.

A live `/v1/pricing` capture requires a Hetzner Cloud project API token. This assignment does not require private credentials, so the fixture intentionally stays offline and synthetic while matching the documented response shape.

## Pricing model

In scope for this connector: **cloud servers**, **volumes**, and **traffic**.

1. **Servers (`server_types`)**
   - Distinct SKUs by server type name (`cx23`, `cax11`, …).
   - Distinct regions by Hetzner location (fixture uses verified EU `fsn1` only).
   - Two commercially meaningful purchase models per location:
     - `hourly` — billed per hour of runtime
     - `monthly` — monthly price cap (bill never exceeds this for a calendar month)
   - Included egress traffic (`included_traffic`, bytes) is location-specific in the live API.

2. **Traffic overage**
   - After included traffic is consumed, egress is billed per TB via `price_per_tb_traffic`.
   - Price can differ by location (e.g. Singapore is higher than EU/US).
   - Emitted as product `Traffic` with `pricing_dimension=egress`.

3. **Volumes**
   - Flat `price_per_gb_month` for block storage; not broken down by location in the API.
   - Emitted as product `Volume`, region `global`, billing unit `GB-month`.

Currency and VAT come from the pricing envelope. Canonical `Price` uses **net** amounts; gross and `vat_rate` are retained in `Attributes`.

## Normalization mapping

| Canonical field | Mapping |
|---|---|
| `Provider` | `hetzner` |
| `Product` | `Cloud Server`, `Volume`, or `Traffic` |
| `SKU` | server type name; `volume`; `{name}-traffic` |
| `ProviderProductID` | server type numeric id, or `volume` |
| `ProviderSKUID` | `{name}-{location}-{hourly\|monthly\|traffic}` or `volume-gb-month` |
| `Region` | Hetzner location name; `global` for volumes |
| `PurchaseModel` | `hourly`, `monthly`, or `on_demand` (volume/traffic) |
| `BillingUnit` | `hour`, `month`, `GB-month`, `TB` |
| `PricingDimension` | `compute`, `storage`, `egress` |
| `Dimensions` | `server_type`, `included_traffic_bytes` when applicable |
| `Attributes` | `vat_rate`, `price_gross`, `billing_cap`, `overage`, … |
| `Source` | type `hetzner-cloud-pricing-api`, URL of the fetch/fixture source |

Out of scope (present in the API but outside the assignment): primary IPs, floating IPs, images, load balancer types, server backups.

## Assumptions and limitations

- Monthly server pricing is modeled as a separate `monthly` purchase model rather than a first-class “cap” field; the relationship to hourly billing is documented via `Attributes["billing_cap"]=monthly`.
- Volume pricing is location-agnostic in the API, so region is `global`.
- Gross/VAT depend on the project that issued the API token; the fixture uses a representative EUR / 19% VAT envelope with gross = net × 1.19.
- Floating-point is never used; decimal strings are preserved from the source.
- Fixture rows are limited to published EU (`fsn1`) reference amounts for `cx23` and `cax11`, plus volume and traffic overage. Unverified CX23 `ash`/`sin` and CPX11 location rows are omitted. Server type IDs follow public `GET /v1/server_types` identifiers (`cx23=114`, `cax11=45`) where available.

## Running tests

```bash
go test ./connectors/hetzner/cloud/
```

Tests load `testdata/price.json` and exercise both normalization unit tests and `testkit.AssertConnectorContract`.
