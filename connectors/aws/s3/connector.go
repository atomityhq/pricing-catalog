package s3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"pricing-catalog/pkg/catalog"
)

const (
	defaultSourceURL = "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonS3/current/index.json"
	sourceType       = "aws-price-list-api"
	defaultProvider  = "aws"
	defaultProduct   = "Amazon S3"
)

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// offerFile represents the root payload of the AWS Price List bulk offer file (formatVersion: "v1.0").
//
// Note: Fields such as FormatVersion, OfferCode, Version, and PublicationDate model the official
// AWS envelope specification for schema completeness and domain documentation. Normalization
// actively consumes Products and Terms to generate canonical pricing records.
type offerFile struct {
	FormatVersion   string             `json:"formatVersion"`
	OfferCode       string             `json:"offerCode"`
	Version         string             `json:"version"`
	PublicationDate string             `json:"publicationDate"`
	Products        map[string]product `json:"products"`
	Terms           terms              `json:"terms"`
}

type product struct {
	SKU           string            `json:"sku"`
	ProductFamily string            `json:"productFamily"`
	Attributes    map[string]string `json:"attributes"`
}

type terms struct {
	OnDemand map[string]map[string]offerTerm `json:"OnDemand"`
}

type offerTerm struct {
	OfferTermCode   string                    `json:"offerTermCode"`
	SKU             string                    `json:"sku"`
	EffectiveDate   string                    `json:"effectiveDate"`
	PriceDimensions map[string]priceDimension `json:"priceDimensions"`
}

type priceDimension struct {
	RateCode     string            `json:"rateCode"`
	Description  string            `json:"description"`
	BeginRange   string            `json:"beginRange"`
	EndRange     string            `json:"endRange"`
	Unit         string            `json:"unit"`
	PricePerUnit map[string]string `json:"pricePerUnit"`
}

// Connector fetches and normalizes Amazon S3 pricing data into canonical records.
type Connector struct {
	reader io.Reader
	client *http.Client
	url    string
}

// New creates an Amazon S3 connector backed by an io.Reader (for offline fixtures and tests).
func New(reader io.Reader) Connector {
	return Connector{
		reader: reader,
		url:    defaultSourceURL,
	}
}

// NewLive creates an Amazon S3 connector that fetches live pricing data from the AWS Price List API.
func NewLive(client *http.Client) Connector {
	if client == nil {
		client = http.DefaultClient
	}
	return Connector{
		client: client,
		url:    defaultSourceURL,
	}
}

// NewWithURL creates an Amazon S3 connector with a custom endpoint URL and HTTP client.
func NewWithURL(client *http.Client, url string) Connector {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(url) == "" {
		url = defaultSourceURL
	}
	return Connector{
		client: client,
		url:    url,
	}
}

// Name returns the stable connector identifier.
func (c Connector) Name() string {
	return "aws-s3"
}

// Fetch reads, parses, and normalizes Amazon S3 pricing data into canonical records.
func (c Connector) Fetch(ctx context.Context) ([]catalog.PricingRecord, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	reader := c.reader
	sourceURL := c.url
	if sourceURL == "" {
		sourceURL = defaultSourceURL
	}

	if reader == nil {
		client := c.client
		if client == nil {
			client = http.DefaultClient
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch pricing from %s: %w", sourceURL, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch pricing from %s: unexpected status code %d", sourceURL, resp.StatusCode)
		}

		return c.parse(ctx, resp.Body, sourceURL)
	}

	return c.parse(ctx, reader, sourceURL)
}

func (c Connector) parse(ctx context.Context, r io.Reader, sourceURL string) ([]catalog.PricingRecord, error) {
	var raw offerFile
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode AWS S3 pricing offer file: %w", err)
	}

	if len(raw.Products) == 0 {
		return nil, fmt.Errorf("pricing offer file contains no products")
	}

	records := make([]catalog.PricingRecord, 0)

	// Sort product SKUs for deterministic iteration
	skus := make([]string, 0, len(raw.Products))
	for sku := range raw.Products {
		skus = append(skus, sku)
	}
	sort.Strings(skus)

	for _, sku := range skus {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		prod := raw.Products[sku]
		attrs := prod.Attributes
		if attrs == nil {
			attrs = make(map[string]string)
		}

		productTerms, exists := raw.Terms.OnDemand[sku]
		if !exists || len(productTerms) == 0 {
			continue
		}

		// Sort term codes for determinism
		termCodes := make([]string, 0, len(productTerms))
		for code := range productTerms {
			termCodes = append(termCodes, code)
		}
		sort.Strings(termCodes)

		for _, termCode := range termCodes {
			term := productTerms[termCode]

			var effectiveFrom *time.Time
			if term.EffectiveDate != "" {
				if t, err := time.Parse(time.RFC3339Nano, term.EffectiveDate); err == nil {
					utc := t.UTC()
					effectiveFrom = &utc
				} else if t, err := time.Parse(time.RFC3339, term.EffectiveDate); err == nil {
					utc := t.UTC()
					effectiveFrom = &utc
				}
			}

			rateCodes := make([]string, 0, len(term.PriceDimensions))
			for rc := range term.PriceDimensions {
				rateCodes = append(rateCodes, rc)
			}
			sort.Strings(rateCodes)

			isTiered := len(term.PriceDimensions) > 1

			for _, rc := range rateCodes {
				pd := term.PriceDimensions[rc]

				currencies := make([]string, 0, len(pd.PricePerUnit))
				for curr := range pd.PricePerUnit {
					currencies = append(currencies, curr)
				}
				sort.Strings(currencies)

				for _, currency := range currencies {
					rawAmount := pd.PricePerUnit[currency]
					amount := strings.TrimSpace(rawAmount)

					// Validate decimal amount
					if !decimalPattern.MatchString(amount) {
						return nil, fmt.Errorf("product %s rate %s: invalid decimal price %q", sku, rc, amount)
					}

					recordSKU := prod.SKU
					if recordSKU == "" {
						recordSKU = sku
					}

					providerProductID := attrs["servicecode"]
					if providerProductID == "" {
						providerProductID = "AmazonS3"
					}

					providerSKUID := pd.RateCode
					if providerSKUID == "" {
						providerSKUID = recordSKU
					}

					region := normalizeRegion(attrs)
					billingUnit := strings.TrimSpace(pd.Unit)
					if billingUnit == "" {
						billingUnit = "unit"
					}

					pricingDim := classifyPricingDimension(
						prod.ProductFamily,
						attrs["usagetype"],
						attrs["group"],
						attrs["operation"],
					)

					dimensions := extractDimensions(attrs)

					var tier *catalog.Tier
					beginRange := strings.TrimSpace(pd.BeginRange)
					endRange := strings.TrimSpace(pd.EndRange)
					if endRange == "Inf" {
						endRange = ""
					}

					if isTiered || (beginRange != "" && beginRange != "0") || endRange != "" {
						if beginRange == "" {
							beginRange = "0"
						}
						tier = &catalog.Tier{
							StartQuantity: beginRange,
							EndQuantity:   endRange,
						}
					}

					retainedAttributes := make(map[string]string, len(attrs)+4)
					for k, v := range attrs {
						if v != "" {
							retainedAttributes[k] = v
						}
					}
					if pd.Description != "" {
						retainedAttributes["description"] = pd.Description
					}
					if pd.RateCode != "" {
						retainedAttributes["rate_code"] = pd.RateCode
					}
					if term.OfferTermCode != "" {
						retainedAttributes["offer_term_code"] = term.OfferTermCode
					}
					if pd.BeginRange != "" {
						retainedAttributes["begin_range"] = pd.BeginRange
					}
					if pd.EndRange != "" {
						retainedAttributes["end_range"] = pd.EndRange
					}

					records = append(records, catalog.PricingRecord{
						Provider:          defaultProvider,
						Product:           defaultProduct,
						SKU:               recordSKU,
						ProviderProductID: providerProductID,
						ProviderSKUID:     providerSKUID,
						Region:            region,
						PurchaseModel:     "on_demand",
						BillingUnit:       billingUnit,
						PricingDimension:  pricingDim,
						Dimensions:        dimensions,
						Price: catalog.Money{
							Amount:   amount,
							Currency: currency,
						},
						Tier:          tier,
						EffectiveFrom: effectiveFrom,
						Source: catalog.Source{
							Type: sourceType,
							URL:  sourceURL,
						},
						Attributes: retainedAttributes,
					})
				}
			}
		}
	}

	return records, nil
}

func normalizeRegion(attrs map[string]string) string {
	if rc := strings.TrimSpace(attrs["regionCode"]); rc != "" {
		return rc
	}
	if toRC := strings.TrimSpace(attrs["toRegionCode"]); toRC != "" {
		return toRC
	}
	if fromRC := strings.TrimSpace(attrs["fromRegionCode"]); fromRC != "" {
		return fromRC
	}

	loc := strings.ToLower(attrs["location"])
	switch {
	case strings.Contains(loc, "n. virginia"):
		return "us-east-1"
	case strings.Contains(loc, "ohio"):
		return "us-east-2"
	case strings.Contains(loc, "n. california"):
		return "us-west-1"
	case strings.Contains(loc, "oregon"):
		return "us-west-2"
	case strings.Contains(loc, "ireland"):
		return "eu-west-1"
	case strings.Contains(loc, "london"):
		return "eu-west-2"
	case strings.Contains(loc, "frankfurt"):
		return "eu-central-1"
	case strings.Contains(loc, "tokyo"):
		return "ap-northeast-1"
	case strings.Contains(loc, "singapore"):
		return "ap-southeast-1"
	case strings.Contains(loc, "sydney"):
		return "ap-southeast-2"
	}

	return "us-east-1"
}

func classifyPricingDimension(family, usagetype, group, operation string) string {
	u := strings.ToLower(usagetype)
	f := strings.ToLower(family)
	g := strings.ToLower(group)
	op := strings.ToLower(operation)

	switch {
	case strings.Contains(u, "retrieval") || strings.Contains(g, "retrieval"):
		return "retrieval"
	case strings.Contains(u, "earlydelete"):
		return "fee"
	case strings.Contains(u, "storagelens") || strings.Contains(u, "inventory") ||
		strings.Contains(u, "storageanalytics") || strings.Contains(u, "batchoperations") ||
		strings.Contains(u, "tagstorage") || op == "storagelens":
		return "management"
	case strings.Contains(u, "request") || strings.Contains(g, "request") ||
		strings.Contains(u, "select") || f == "api request":
		return "request"
	case strings.Contains(u, "storage") || strings.Contains(u, "bytehrs") || f == "storage":
		return "storage"
	case f == "data transfer" || strings.Contains(u, "datatransfer") ||
		strings.Contains(u, "s3rtc") || strings.Contains(u, "mrap"):
		return "data_transfer"
	case f == "fee" || strings.Contains(u, "fee"):
		return "fee"
	default:
		return "storage"
	}
}

func extractDimensions(attrs map[string]string) map[string]string {
	dims := make(map[string]string)

	storageClass := resolveStorageClass(attrs)
	if storageClass != "" {
		dims["storage_class"] = storageClass
	}

	if vt := attrs["volumeType"]; vt != "" {
		dims["volume_type"] = vt
	}
	if op := attrs["operation"]; op != "" {
		dims["operation"] = op
	}
	if ut := attrs["usagetype"]; ut != "" {
		dims["usagetype"] = ut
	}
	if grp := attrs["group"]; grp != "" {
		dims["group"] = grp
	}

	tierType := extractTierType(attrs["usagetype"], attrs["group"])
	if tierType != "" {
		dims["tier_type"] = tierType
	}

	return dims
}

func resolveStorageClass(attrs map[string]string) string {
	vt := attrs["volumeType"]
	sc := attrs["storageClass"]
	ut := attrs["usagetype"]

	switch {
	case strings.Contains(vt, "Express One Zone") || strings.Contains(ut, "-XZ-"):
		return "Express One Zone"
	case strings.Contains(vt, "Deep Archive") || strings.Contains(ut, "GDA"):
		return "Glacier Deep Archive"
	case strings.Contains(vt, "Glacier Instant") || strings.Contains(ut, "GIR"):
		return "Glacier Instant Retrieval"
	case strings.Contains(vt, "Amazon Glacier") || strings.Contains(ut, "Glacier"):
		return "Glacier Flexible Retrieval"
	case strings.Contains(vt, "One Zone - Infrequent Access") || strings.Contains(ut, "ZIA"):
		return "One Zone-IA"
	case strings.Contains(vt, "Standard - Infrequent Access") || strings.Contains(ut, "SIA"):
		return "Standard-IA"
	case strings.Contains(vt, "Intelligent") || strings.Contains(sc, "Intelligent") || strings.Contains(ut, "INT"):
		return "Intelligent-Tiering"
	case strings.Contains(vt, "Reduced Redundancy") || strings.Contains(ut, "RRS"):
		return "Reduced Redundancy"
	case vt == "Standard" || sc == "General Purpose" || strings.Contains(ut, "TimedStorage-ByteHrs"):
		return "Standard"
	case sc != "" && sc != "General Purpose":
		return sc
	default:
		return ""
	}
}

var tierTypeRegex = regexp.MustCompile(`(?i)(Tier\d+)`)

func extractTierType(usageType, group string) string {
	if m := tierTypeRegex.FindString(usageType); m != "" {
		return strings.ToUpper(m[:1]) + strings.ToLower(m[1:])
	}
	if m := tierTypeRegex.FindString(group); m != "" {
		return strings.ToUpper(m[:1]) + strings.ToLower(m[1:])
	}
	return ""
}
