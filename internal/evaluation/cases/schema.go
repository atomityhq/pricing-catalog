package cases

// Suite describes the private evaluation cases for one candidate task.
//
// The serialized representation is YAML. These types intentionally contain
// only evaluation semantics; provider-specific evaluation logic belongs in
// the evaluator.
type Suite struct {
	Version int    `yaml:"version"`
	Task    string `yaml:"task"`
	Cases   []Case `yaml:"cases"`
}

type Case struct {
	ID          string       `yaml:"id"`
	Category    string       `yaml:"category"`
	Description string       `yaml:"description"`
	Input       Input        `yaml:"input"`
	Expect      Expectations `yaml:"expect"`
}

type Input struct {
	Fixture string `yaml:"fixture"`
}

type Expectations struct {
	Records       []RecordExpectation `yaml:"records"`
	Error         bool                `yaml:"error"`
	Deterministic bool                `yaml:"deterministic"`
}

type RecordExpectation struct {
	Match  FieldMatchers `yaml:"match"`
	Values RecordValues  `yaml:"values"`
	Price  *PriceValue   `yaml:"price"`
}

// FieldMatchers describes which fields must participate in record matching.
// Supported values are intentionally small and evaluator-defined.
type FieldMatchers struct {
	Provider         string `yaml:"provider"`
	Product          string `yaml:"product"`
	SKU              string `yaml:"sku"`
	Region           string `yaml:"region"`
	PurchaseModel    string `yaml:"purchase_model"`
	BillingUnit      string `yaml:"billing_unit"`
	PricingDimension string `yaml:"pricing_dimension"`
}

type RecordValues struct {
	Provider         string `yaml:"provider"`
	Product          string `yaml:"product"`
	SKU              string `yaml:"sku"`
	Region           string `yaml:"region"`
	PurchaseModel    string `yaml:"purchase_model"`
	BillingUnit      string `yaml:"billing_unit"`
	PricingDimension string `yaml:"pricing_dimension"`
}

type PriceValue struct {
	Amount   string `yaml:"amount"`
	Currency string `yaml:"currency"`
}
