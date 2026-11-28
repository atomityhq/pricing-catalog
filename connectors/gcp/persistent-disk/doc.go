// Package gcppersistentdisk implements a pricing-catalog connector for the
// Google Cloud Persistent Disk domain (services/6F81-5844-456A).
//
// It fetches SKU pricing data from the Google Cloud Billing Catalog API v1
// (or a deterministic fixture reader) and normalizes zonal and regional
// persistent disks (Standard, Balanced, SSD, Extreme, including provisioned
// IOPS) and snapshots (standard, archive, instant) into canonical
// catalog.PricingRecord values.
package gcppersistentdisk
