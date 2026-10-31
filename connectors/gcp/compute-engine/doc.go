// Package gcpcomputeengine implements a pricing-catalog connector for the
// Google Cloud Compute Engine domain (services/6F81-5844-456A).
//
// It fetches SKU pricing data from the Google Cloud Billing Catalog API v1
// (or a deterministic fixture reader) and normalizes predefined and custom VM
// shapes, per-resource rates (vCPU, memory, GPU, Local SSD), purchase models
// (on-demand, committed use, spot/preemptible), and OS license tiers into
// canonical catalog.PricingRecord values.
package gcpcomputeengine
