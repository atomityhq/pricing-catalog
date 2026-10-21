# Amazon S3 Connector Design

## Source

The connector integrates with the **AWS Price List Bulk API (Offer File version 1.0)**:

- **Endpoint (Global / Default)**: `https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonS3/current/index.json`
- **Endpoint (Regional index)**: `https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonS3/current/<region>/index.json`
- **Source Type**: `aws-price-list-api`

This is the authoritative, public, machine-readable catalog published directly by AWS. It requires no private API keys or AWS credentials.

For deterministic testing and maintainer evaluation, the connector supports constructor injection (`New(reader io.Reader)`) using captured sanitized fixtures in `testdata/pricing.json`, while supporting live production execution through `NewLive(*http.Client)`.

---

## Pricing Model

Amazon S3's pricing model encompasses several distinct pricing dimensions and cost drivers:

1. **Storage Classes & Tiered Capacity Pricing**:
   - **S3 Standard**: High-durability general purpose storage with tiered volume pricing:
     - 0 to 51,200 GB (First 50 TB/month): $0.023 / GB-Mo
     - 51,200 to 512,000 GB (Next 450 TB/month): $0.022 / GB-Mo
     - Over 512,000 GB (>500 TB/month): $0.021 / GB-Mo
   - **S3 Intelligent-Tiering**: Automatic cost optimization across access tiers (Frequent, Infrequent, Archive Instant, Archive, and Deep Archive).
   - **S3 Standard-Infrequent Access (Standard-IA)**: Lower storage rate ($0.0125 / GB-Mo) with retrieval fee.
   - **S3 One Zone-Infrequent Access (One Zone-IA)**: Single-AZ lower storage rate ($0.0100 / GB-Mo).
   - **S3 Glacier Instant Retrieval**: Millisecond retrieval for archive storage ($0.0040 / GB-Mo).
   - **S3 Glacier Flexible Retrieval (formerly Amazon Glacier)**: Low-cost cold storage ($0.0036 / GB-Mo) with flexible retrieval options.
   - **S3 Glacier Deep Archive**: Lowest-cost cloud archive ($0.00099 / GB-Mo).
   - **S3 Express One Zone**: High performance low-latency storage ($0.16 / GB-Mo).
   - **S3 Reduced Redundancy (RRS)**: Legacy non-critical storage.

2. **API Requests by Operation Type**:
   - **Tier 1 Requests**: `PUT`, `COPY`, `POST`, `LIST` operations across storage classes (`Requests-Tier1`, `Requests-SIA-Tier1`, `Requests-GIR-Tier1`, etc.).
   - **Tier 2 Requests**: `GET`, `SELECT`, and other read requests (`Requests-Tier2`, `Requests-SIA-Tier2`, `Requests-GIR-Tier2`, etc.).
   - **Tier 3 / Tier 5 Requests**: Cold archive retrieval requests (Glacier Flexible and Deep Archive).
   - **Tier 4 Requests**: Lifecycle transitions between storage classes.
   - **Tier 6 / Tier 8 Requests**: Expedited and Standard Glacier restore requests.

3. **Data Retrieval Charges**:
   - Per-GB retrieval fees for infrequent access and archive tiers (e.g. Standard-IA, One Zone-IA, Glacier Instant Retrieval, Glacier Flexible Retrieval, and Glacier Deep Archive).
   - S3 Select scanned and returned bytes.

4. **Minimum Storage Duration & Minimum Object Size Rules**:
   - Early deletion prorated fees for objects deleted or overwritten prior to the minimum storage retention commitment:
     - Standard-IA and One Zone-IA: 30 days minimum (`EarlyDelete-SIA`, `EarlyDelete-ZIA`)
     - Glacier Instant Retrieval: 90 days minimum (`EarlyDelete-GIR`)
     - Glacier Flexible Retrieval: 90 days minimum (`EarlyDelete-ByteHrs`)
     - Glacier Deep Archive: 180 days minimum (`EarlyDelete-GDA`)
   - Minimum billable object size rules (128 KB for IA/GIR classes) captured in attributes.

5. **Management & Analytics**:
   - **S3 Storage Lens**: Multi-tiered advanced metric charges per million objects monitored (0-25B, 25B-100B, >100B).
   - **S3 Inventory**: Per-object charge for objects listed in inventory reports.
   - **S3 Storage Class Analysis**: Per-object charge for analytics tracking.
   - **S3 Object Tagging**: Per 10,000 tags per month.
   - **S3 Batch Operations**: Per-job fee plus per-operation charge.

---

## Mapping Decisions

| Canonical Field | Source in AWS S3 | Description & Normalization |
|---|---|---|
| `Provider` | `"aws"` | Canonical provider identifier. |
| `Product` | `"Amazon S3"` | Human-readable canonical product name. |
| `SKU` | `product.sku` | Provider SKU identifier (e.g. `WP9ANXZGBYYSGJEA`). |
| `ProviderProductID` | `attributes.servicecode` | Provider service product identifier (`"AmazonS3"`). |
| `ProviderSKUID` | `priceDimension.rateCode` | Unique AWS rate code (e.g. `WP9ANXZGBYYSGJEA.JRTCKXETXF.PGHJ3S3EYE`). |
| `Region` | `attributes.regionCode` | Normalized region code (e.g. `us-east-1`). Falls back to `toRegionCode`/`fromRegionCode` or location mapping if blank. |
| `PurchaseModel` | `"on_demand"` | AWS S3 follows an on-demand consumption model. |
| `BillingUnit` | `priceDimension.unit` | Normalized billing unit (`GB-Mo`, `Requests`, `GB`, `Objects`, `Tag-Mo`, `Job`). |
| `PricingDimension` | Derived | Classifies dimension into `"storage"`, `"request"`, `"retrieval"`, `"management"`, `"fee"`, or `"data_transfer"`. |
| `Dimensions` | Multiple attributes | Preserves `storage_class`, `volume_type`, `operation`, `usagetype`, `group`, and `tier_type`. |
| `Price` | `priceDimension.pricePerUnit` | Exact decimal string and currency (e.g. `Amount: "0.0230000000"`, `Currency: "USD"`). Never parsed as float. |
| `Tier` | `priceDimension.beginRange` & `endRange` | Preserved for tiered pricing (e.g. S3 Standard capacity, Storage Lens). Open-ended final tiers omit `EndQuantity`. |
| `EffectiveFrom` | `offerTerm.effectiveDate` | Parsed ISO 8601 UTC timestamp. |
| `Source` | Type & URL | `aws-price-list-api` with the authoritative AWS Price List endpoint URL. |
| `Attributes` | Provider attributes | Retains all provider metadata (`description`, `feeCode`, `feeDescription`, `availability`, `durability`, `rate_code`, `offer_term_code`, etc.). |

---

## Assumptions and Limitations

1. **Currencies**: AWS Offer files for S3 primarily publish prices in `USD`. If additional currencies are present in `pricePerUnit`, each currency is emitted as a distinct canonical record.
2. **Replication Data Transfer**: Data transfer incurred by S3 Replication Time Control (RTC) or Multi-Region Access Points (MRAP) included in the S3 offer file is mapped with `pricing_dimension: "data_transfer"`. Full inter-region egress is covered under the dedicated `aws-data-transfer` domain.
3. **Free Tier**: Free tier allowances are documented in AWS terms and footnotes but are not modeled as standalone pricing SKUs in the canonical catalog.
4. **Reserved / Savings Plans**: S3 storage does not utilize EC2-style Savings Plans or Reserved Instances; all pricing is on-demand with volume discounts modeled via canonical tiers.

---

## Testing

Run tests with:

```bash
# Run S3 connector tests
go test -v ./connectors/aws/s3/...

# Run contract tests and repository checks
make check
```
