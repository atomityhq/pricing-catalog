package cases

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Suite describes the private evaluation cases for one candidate task.
//
// The serialized representation is YAML. These types intentionally contain
// only evaluation semantics; provider-specific evaluation logic belongs in
// the evaluator.
type Suite struct {
	Version int     `yaml:"version"`
	Task    string  `yaml:"task"`
	Cases   []*Case `yaml:"cases"`
}

type Case struct {
	ID          string        `yaml:"id"`
	Category    string        `yaml:"category"`
	Description string        `yaml:"description"`
	Input       *Input        `yaml:"input"`
	Expect      *Expectations `yaml:"expect"`
}

type Input struct {
	Fixture string `yaml:"fixture"`
}

type Expectations struct {
	Records       []*RecordExpectation `yaml:"records"`
	Error         bool                 `yaml:"error"`
	Deterministic bool                 `yaml:"deterministic"`
}

// RecordExpectation uses Match to identify the expected canonical record.
//
// For example:
//
//	match:
//	  sku: exact
//	  region: exact
//
//	values:
//	  sku: ABC123
//	  region: us-east-1
//
// Match controls record selection. Values and Price control assertions about
// the selected record.
type RecordExpectation struct {
	Match  *FieldMatchers `yaml:"match"`
	Values *RecordValues  `yaml:"values"`
	Price  *PriceValue    `yaml:"price"`
}

// FieldMatchers describes which fields must participate in record matching.
// Supported values are intentionally small and evaluator-defined.
type FieldMatchers struct {
	Provider          string `yaml:"provider"`
	Product           string `yaml:"product"`
	SKU               string `yaml:"sku"`
	ProviderProductID string `yaml:"provider_product_id"`
	ProviderSKUID     string `yaml:"provider_sku_id"`
	Region            string `yaml:"region"`
	PurchaseModel     string `yaml:"purchase_model"`
	BillingUnit       string `yaml:"billing_unit"`
	PricingDimension  string `yaml:"pricing_dimension"`
}

type RecordValues struct {
	Provider          string            `yaml:"provider"`
	Product           string            `yaml:"product"`
	SKU               string            `yaml:"sku"`
	ProviderProductID string            `yaml:"provider_product_id"`
	ProviderSKUID     string            `yaml:"provider_sku_id"`
	Region            string            `yaml:"region"`
	PurchaseModel     string            `yaml:"purchase_model"`
	BillingUnit       string            `yaml:"billing_unit"`
	PricingDimension  string            `yaml:"pricing_dimension"`
	Dimensions        map[string]string `yaml:"dimensions"`
	Attributes        map[string]string `yaml:"attributes"`
	Tier              *TierValues       `yaml:"tier"`
	EffectiveFrom     string            `yaml:"effective_from"`
	EffectiveTo       string            `yaml:"effective_to"`
	Source            *SourceValues     `yaml:"source"`
}

type TierValues struct {
	StartQuantity string `yaml:"start_quantity"`
	EndQuantity   string `yaml:"end_quantity"`
}

type SourceValues struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
}

type PriceValue struct {
	Amount   string `yaml:"amount"`
	Currency string `yaml:"currency"`
}

func Load(r io.Reader) (*Suite, error) {
	if r == nil {
		return nil, errors.New("evaluation case reader is nil")
	}

	var suite Suite
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(&suite); err != nil {
		return nil, fmt.Errorf("decode evaluation cases: %w", err)
	}

	if err := suite.Validate(); err != nil {
		return nil, err
	}

	return &suite, nil
}

func (s *Suite) Validate() error {
	if s == nil {
		return errors.New("evaluation suite is nil")
	}
	if s.Version != 1 {
		return fmt.Errorf("unsupported evaluation suite version %d", s.Version)
	}
	if strings.TrimSpace(s.Task) == "" {
		return errors.New("evaluation suite task is required")
	}
	if len(s.Cases) == 0 {
		return errors.New("evaluation suite must contain at least one case")
	}

	seen := make(map[string]struct{}, len(s.Cases))
	for i, c := range s.Cases {
		if c == nil {
			return fmt.Errorf("case %d is nil", i)
		}
		if strings.TrimSpace(c.ID) == "" {
			return fmt.Errorf("case %d: id is required", i)
		}
		if _, ok := seen[c.ID]; ok {
			return fmt.Errorf("duplicate evaluation case id %q", c.ID)
		}
		seen[c.ID] = struct{}{}

		if strings.TrimSpace(c.Category) == "" {
			return fmt.Errorf("case %q: category is required", c.ID)
		}
		if c.Expect == nil {
			return fmt.Errorf("case %q: expectations are required", c.ID)
		}
		if c.Input == nil || strings.TrimSpace(c.Input.Fixture) == "" {
			return fmt.Errorf("case %q: input.fixture is required", c.ID)
		}

		if c.Expect.Error {
			continue
		}
		for j, record := range c.Expect.Records {
			if record == nil {
				return fmt.Errorf("case %q: record expectation %d is nil", c.ID, j)
			}
			if record.Match == nil {
				return fmt.Errorf("case %q: record expectation %d: match is required", c.ID, j)
			}
			if !record.Match.HasMatcher() {
				return fmt.Errorf("case %q: record expectation %d: match must identify at least one field", c.ID, j)
			}
		}
	}

	return nil
}

func (m *FieldMatchers) HasMatcher() bool {
	if m == nil {
		return false
	}
	return m.Provider != "" ||
		m.Product != "" ||
		m.SKU != "" ||
		m.ProviderProductID != "" ||
		m.ProviderSKUID != "" ||
		m.Region != "" ||
		m.PurchaseModel != "" ||
		m.BillingUnit != "" ||
		m.PricingDimension != ""
}

func ParseTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("invalid RFC3339 timestamp %q: %w", value, err)
	}
	t = t.UTC()
	return &t, nil
}
