package catalog

import "time"

// Snapshot is the versioned catalog payload distributed to consumers.
type Snapshot struct {
	Version     string          `json:"version"`
	GeneratedAt time.Time       `json:"generated_at"`
	Records     []PricingRecord `json:"records"`
}

// PricingRecord is the canonical representation of one provider price.
//
// A record represents a specific SKU/pricing combination. Provider-specific
// dimensions that are commercially meaningful but do not deserve first-class
// fields yet can be preserved in Dimensions and Attributes.
type PricingRecord struct {
	Provider string `json:"provider"`
	Product  string `json:"product"`
	SKU      string `json:"sku"`

	ProviderProductID string `json:"provider_product_id,omitempty"`
	ProviderSKUID     string `json:"provider_sku_id,omitempty"`

	Region        string `json:"region"`
	PurchaseModel string `json:"purchase_model"`
	BillingUnit   string `json:"billing_unit"`

	PricingDimension string            `json:"pricing_dimension"`
	Dimensions       map[string]string `json:"dimensions,omitempty"`

	Price Money `json:"price"`
	Tier  *Tier `json:"tier,omitempty"`

	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`

	Source     Source            `json:"source"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Money uses a decimal string instead of float64 so catalog data does not
// lose precision during normalization or serialization.
type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// Tier describes a quantity-based pricing tier. EndQuantity is nil for an
// open-ended final tier.
type Tier struct {
	StartQuantity string `json:"start_quantity"`
	EndQuantity   string `json:"end_quantity,omitempty"`
}

// Source records the authoritative source used to obtain a pricing record.
//
// Refresh/build time belongs to Snapshot.GeneratedAt rather than individual
// records. Keeping a timestamp on every record would make every record appear
// changed on every refresh even when the actual pricing is unchanged.
type Source struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}
