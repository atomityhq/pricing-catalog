# Google Compute Engine connector design

## Source

The authoritative public pricing source for Google Compute Engine is the **Google Cloud Billing Catalog API v1**:

- **Service Identifier:** `services/6F81-5844-456A` (Compute Engine)
- **REST Endpoint:** `https://cloudbilling.googleapis.com/v1/services/6F81-5844-456A/skus`

In production refreshes, the connector fetches paginated SKU responses (`nextPageToken`) over HTTPS using `http.Client`. To keep unit and contract tests deterministic and free of private credentials, the connector supports reading captured JSON payloads via `io.Reader` (`WithReader`) and local test servers (`WithBaseURL`) backed by `testdata/source.json`.

## Pricing model

Unlike cloud providers that price monolithic VM instance sizes under a single SKU, Google Compute Engine uses a **component-based (per-resource) pricing model**:

1. **Per-resource billing (`PricingDimension`):** Virtual machine capacity is billed as atomic resource SKUs across `vcpu`, `memory`, attached `gpu` accelerators, `local_ssd` storage, and commercial operating system `license` premiums.
2. **Machine families and shapes (`Dimensions`):** Compute SKUs belong to specific machine series (`n2`, `n2d`, `e2`, `c3`, `a2`, etc.) and distinguish between `predefined` shapes and `custom` shapes (including `extended` memory pricing when the memory-to-vCPU ratio exceeds standard family limits).
3. **Purchase models (`PurchaseModel`):**
   - `on_demand`: Standard pay-as-you-go resource usage (`category.usageType: "OnDemand"`).
   - `committed`: Resource-based Committed Use Discounts (`Commit1Yr`, `Commit3Yr`, `Commit1Mo`), with the contract duration preserved in `Dimensions["commitment_term"]`.
   - `spot` and `preemptible`: Discounted interruptible capacity (`category.usageType: "Preemptible"`), differentiated via SKU metadata and preserved in `Dimensions["provisioning_model"]`.
4. **Tiered rates (`Tier`):** SKUs such as commercial OS licenses (e.g., RHEL, SLES, Windows Server) expose multiple brackets in `pricingExpression.tieredRates` based on usage thresholds or core counts.

## Mapping decisions

- **Scope filtering:** The Compute Engine service catalog (`services/6F81-5844-456A`) also contains networking (NAT, Load Balancing, egress) and standard persistent disk SKUs. The normalizer filters by `category.resourceFamily` (`Compute`, `License`, and `Storage` where `resourceGroup` is `LocalSSD`) to match the VM instance domain scope.
- **Provider & Product identity:** `Provider` is normalized to `"gcp"`. `ProviderProductID` retains `"services/6F81-5844-456A"`, and `ProviderSKUID` / `SKU` retain the upstream `skuId`. `Product` is mapped to `"Compute Engine VM"`, `"Compute Engine GPU"`, `"Compute Engine Local SSD"`, or `"Compute Engine OS License"`.
- **Region unrolling:** A single GCP SKU frequently lists multiple regions in `serviceRegions` (or `geoTaxonomy.regions`). The normalizer unrolls each unique region into a distinct `PricingRecord` so consumers can query prices by canonical region without losing granularity.
- **Exact decimal conversion:** GCP represents unit prices as integer `units` (string) and `nanos` (`int32`, $10^{-9}$). The connector formats these components directly into exact decimal strings without `float64` arithmetic.
- **Tier interval construction:** GCP specifies only the lower bound (`startUsageAmount`) for each element in `tieredRates`. The connector maps tier `i`'s `StartQuantity` from `rates[i].startUsageAmount` and populates `EndQuantity` from `rates[i+1].startUsageAmount` (leaving `EndQuantity` empty for the final open-ended tier).
- **Provenance and metadata:** Raw provider fields (`service_provider_name`, `resource_family`, `resource_group`, `usage_type`, `geo_taxonomy_type`, `base_unit`, `aggregation_interval`, `aggregation_level`) are retained in `Attributes`, while `EffectiveFrom` preserves `pricingInfo.effectiveTime` in UTC.
- **Deterministic output:** All normalized records are sorted by `catalog.RecordKey` prior to returning from `Fetch`.

## Assumptions and limitations

- **Composite VM cost assembly:** Because the upstream GCP API exposes atomic vCPU-hour and GiB-hour SKUs rather than bundled machine-type SKUs (such as `n2-standard-4`), computing the total hourly cost of a complete VM shape requires multiplying and summing the respective vCPU, memory, GPU, Local SSD, and OS license records at the consumer layer.
- **Sustained Use Discounts (SUDs) & Spend-based Flexible CUDs:** Automatic monthly usage-duration discounts (SUDs) and billing-account-level spend commitments depend on dynamic account-wide consumption rather than static SKU unit rates, and therefore cannot be represented as static catalog SKU records.
- **Description heuristics:** GCP's Catalog API does not expose dedicated structured fields for machine series names, Spot vs. legacy Preemptible differentiation, or Extended Memory flags; these dimensions are extracted deterministically from `category.resourceGroup` and `description`.
