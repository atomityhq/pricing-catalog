package dynamodb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// sourceProduct is the provider-specific product entry from the AWS offer file.
// Attributes are retained as strings because AWS adds and changes metadata over time.
type sourceProduct struct {
	SKU           string            `json:"sku"`
	ProductFamily string            `json:"productFamily"`
	Attributes    map[string]string `json:"attributes"`
}

// sourcePriceDimension is one billable rate within an AWS term.
type sourcePriceDimension struct {
	RateCode     string            `json:"rateCode"`
	Description  string            `json:"description"`
	Unit         string            `json:"unit"`
	BeginRange   string            `json:"beginRange"`
	EndRange     string            `json:"endRange"`
	PricePerUnit map[string]string `json:"pricePerUnit"`
}

// sourceTerm represents one OnDemand or Reserved term for a product SKU.
type sourceTerm struct {
	TermCode        string
	OfferTermCode   string
	SKU             string
	EffectiveDate   string
	TermType        string
	TermAttributes  map[string]string
	PriceDimensions []sourcePriceDimension
}

// decodeOfferFile reads the AWS offer document as a stream. The AWS offer format
// places products before terms; terms are matched to products by SKU.
func decodeOfferFile(ctx context.Context, r io.Reader) (map[string]sourceProduct, []sourceTerm, error) {
	dec := json.NewDecoder(r)

	tok, err := dec.Token()
	if err != nil {
		return nil, nil, fmt.Errorf("offer file: expected opening object: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, nil, fmt.Errorf("offer file: expected '{', got %v", tok)
	}

	var products map[string]sourceProduct
	var terms []sourceTerm

	for dec.More() {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		tok, err = dec.Token()
		if err != nil {
			return nil, nil, fmt.Errorf("offer file: reading section name: %w", err)
		}
		section, ok := tok.(string)
		if !ok {
			return nil, nil, fmt.Errorf("offer file: expected section name string, got %T", tok)
		}

		switch section {
		case "products":
			products, err = parseProducts(ctx, dec)
			if err != nil {
				return nil, nil, err
			}
		case "terms":
			if products == nil {
				return nil, nil, fmt.Errorf("offer file: 'terms' section appeared before 'products'")
			}
			err = parseTerms(ctx, dec, products, func(term sourceTerm) error {
				terms = append(terms, term)
				return nil
			})
			if err != nil {
				return nil, nil, err
			}
		default:
			// Ignore metadata such as formatVersion, publicationDate and attributesList.
			if err := skipValue(dec); err != nil {
				return nil, nil, fmt.Errorf("offer file: skipping section %q: %w", section, err)
			}
		}
	}

	tok, err = dec.Token()
	if err != nil {
		return nil, nil, fmt.Errorf("offer file: expected closing object: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '}' {
		return nil, nil, fmt.Errorf("offer file: expected '}', got %v", tok)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, nil, fmt.Errorf("offer file: contains multiple JSON values")
		}
		return nil, nil, fmt.Errorf("offer file: trailing data: %w", err)
	}
	if products == nil {
		return nil, nil, fmt.Errorf("offer file: missing 'products' section")
	}

	return products, terms, nil
}

func parseProducts(ctx context.Context, dec *json.Decoder) (map[string]sourceProduct, error) {
	if err := expectDelimiter(dec, '{', "products"); err != nil {
		return nil, err
	}

	products := make(map[string]sourceProduct)
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("products: reading SKU key: %w", err)
		}
		sku, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("products: expected string SKU key, got %T", tok)
		}

		var product sourceProduct
		if err := dec.Decode(&product); err != nil {
			return nil, fmt.Errorf("products: decoding SKU %q: %w", sku, err)
		}
		if product.SKU == "" {
			return nil, fmt.Errorf("products: SKU %q has no product sku field", sku)
		}
		if product.SKU != sku {
			return nil, fmt.Errorf("products: product sku %q does not match key %q", product.SKU, sku)
		}
		products[sku] = product
	}

	if err := consumeClosingDelimiter(dec, '}', "products"); err != nil {
		return nil, err
	}
	return products, nil
}

func parseTerms(ctx context.Context, dec *json.Decoder, products map[string]sourceProduct, emit func(sourceTerm) error) error {
	if err := expectDelimiter(dec, '{', "terms"); err != nil {
		return err
	}

	for dec.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("terms: reading category: %w", err)
		}
		category, ok := tok.(string)
		if !ok {
			return fmt.Errorf("terms: expected category string, got %T", tok)
		}

		switch category {
		case "OnDemand", "Reserved":
			if err := parseTermCategory(ctx, dec, category, products, emit); err != nil {
				return err
			}
		default:
			// Future AWS term categories are outside this connector's current scope.
			if err := skipValue(dec); err != nil {
				return fmt.Errorf("terms: skipping category %q: %w", category, err)
			}
		}
	}
	return consumeClosingDelimiter(dec, '}', "terms")
}

func parseTermCategory(ctx context.Context, dec *json.Decoder, category string, products map[string]sourceProduct, emit func(sourceTerm) error) error {
	if err := expectDelimiter(dec, '{', "terms."+category); err != nil {
		return err
	}

	for dec.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("terms.%s: reading SKU key: %w", category, err)
		}
		sku, ok := tok.(string)
		if !ok {
			return fmt.Errorf("terms.%s: expected SKU string, got %T", category, tok)
		}

		if _, found := products[sku]; !found {
			if err := skipValue(dec); err != nil {
				return fmt.Errorf("terms.%s: skipping unknown SKU %q: %w", category, sku, err)
			}
			continue
		}
		if err := parseTermsForSKU(ctx, dec, category, sku, emit); err != nil {
			return err
		}
	}
	return consumeClosingDelimiter(dec, '}', "terms."+category)
}

func parseTermsForSKU(ctx context.Context, dec *json.Decoder, category, sku string, emit func(sourceTerm) error) error {
	if err := expectDelimiter(dec, '{', fmt.Sprintf("terms.%s.%s", category, sku)); err != nil {
		return err
	}

	for dec.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("terms.%s.%s: reading term key: %w", category, sku, err)
		}
		termKey, ok := tok.(string)
		if !ok {
			return fmt.Errorf("terms.%s.%s: expected term key string, got %T", category, sku, tok)
		}

		var raw struct {
			OfferTermCode   string                          `json:"offerTermCode"`
			SKU             string                          `json:"sku"`
			EffectiveDate   string                          `json:"effectiveDate"`
			PriceDimensions map[string]sourcePriceDimension `json:"priceDimensions"`
			TermAttributes  map[string]string               `json:"termAttributes"`
		}
		if err := dec.Decode(&raw); err != nil {
			return fmt.Errorf("terms.%s.%s.%s: decoding term: %w", category, sku, termKey, err)
		}
		if raw.SKU != sku {
			return fmt.Errorf("terms.%s.%s.%s: term sku %q does not match outer sku", category, sku, termKey, raw.SKU)
		}
		if raw.OfferTermCode == "" {
			return fmt.Errorf("terms.%s.%s.%s: missing offerTermCode", category, sku, termKey)
		}
		if len(raw.PriceDimensions) == 0 {
			return fmt.Errorf("terms.%s.%s.%s: missing priceDimensions", category, sku, termKey)
		}

		term := sourceTerm{
			TermCode:       termKey,
			OfferTermCode:  raw.OfferTermCode,
			SKU:            raw.SKU,
			EffectiveDate:  raw.EffectiveDate,
			TermType:       category,
			TermAttributes: raw.TermAttributes,
		}
		for _, key := range sortedKeys(raw.PriceDimensions) {
			dim := raw.PriceDimensions[key]
			if dim.RateCode == "" {
				return fmt.Errorf("terms.%s.%s rate %q: missing rateCode", category, sku, key)
			}
			if dim.RateCode != key {
				return fmt.Errorf(
					"terms.%s.%s rate key %q does not match rateCode %q",
					category, sku, key, dim.RateCode,
				)
			}
			if len(dim.PricePerUnit) == 0 {
				return fmt.Errorf("terms.%s.%s rate %q: missing pricePerUnit", category, sku, dim.RateCode)
			}
			term.PriceDimensions = append(term.PriceDimensions, dim)
		}
		if err := emit(term); err != nil {
			return err
		}
	}
	return consumeClosingDelimiter(dec, '}', fmt.Sprintf("terms.%s.%s", category, sku))
}

func expectDelimiter(dec *json.Decoder, want json.Delim, path string) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("%s: expected opening %q: %w", path, want, err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != want {
		return fmt.Errorf("%s: expected %q, got %v", path, want, tok)
	}
	return nil
}

func consumeClosingDelimiter(dec *json.Decoder, want json.Delim, path string) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("%s: expected closing %q: %w", path, want, err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != want {
		return fmt.Errorf("%s: expected %q, got %v", path, want, tok)
	}
	return nil
}

// skipValue consumes one complete JSON value without allocating a representation.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil // scalar value
	}

	switch delim {
	case '{':
		for dec.More() {
			if _, err := dec.Token(); err != nil { // object key
				return err
			}
			if err := skipValue(dec); err != nil {
				return err
			}
		}
		return consumeClosingDelimiter(dec, '}', "skipped object")
	case '[':
		for dec.More() {
			if err := skipValue(dec); err != nil {
				return err
			}
		}
		return consumeClosingDelimiter(dec, ']', "skipped array")
	default:
		return fmt.Errorf("unexpected delimiter %q while skipping JSON value", delim)
	}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
