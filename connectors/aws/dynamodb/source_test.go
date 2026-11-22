package dynamodb

import (
	"context"
	"strings"
	"testing"
)

func TestDecodeOfferFile(t *testing.T) {
	input := `{
		"formatVersion":"v1.0",
		"products":{
			"SKU-1":{"sku":"SKU-1","productFamily":"Database Storage","attributes":{"regionCode":"us-east-1","tableClass":"Standard"}},
			"SKU-2":{"sku":"SKU-2","productFamily":"API Request","attributes":{"regionCode":"eu-west-1"}}
		},
		"terms":{
			"OnDemand":{
				"SKU-1":{
					"TERM-B":{"offerTermCode":"OD","sku":"SKU-1","effectiveDate":"2026-01-01T00:00:00Z","priceDimensions":{
						"RATE-Z":{"rateCode":"RATE-Z","description":"second","unit":"GB-Mo","beginRange":"0","endRange":"Inf","pricePerUnit":{"USD":"0.25"}},
						"RATE-A":{"rateCode":"RATE-A","description":"first","unit":"GB-Mo","beginRange":"0","endRange":"Inf","pricePerUnit":{"USD":"0.00"}}
					},"termAttributes":{}}
				},
				"UNKNOWN-SKU":{"IGNORED":{"anything":true}}
			},
			"Reserved":{
				"SKU-2":{
					"TERM-R":{"offerTermCode":"RES","sku":"SKU-2","effectiveDate":"2026-01-01T00:00:00Z","priceDimensions":{
						"RATE-R":{"rateCode":"RATE-R","description":"reserved","unit":"Quantity","beginRange":"0","endRange":"Inf","pricePerUnit":{"USD":"10.00"}}
					},"termAttributes":{"LeaseContractLength":"1yr","PurchaseOption":"Partial Upfront"}}
				}
			}
		},
		"futureSection":{"nested":[1,{"x":true}]}
	}`

	products, terms, err := decodeOfferFile(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("decodeOfferFile() error = %v", err)
	}
	if len(products) != 2 {
		t.Fatalf("got %d products, want 2", len(products))
	}
	if len(terms) != 2 {
		t.Fatalf("got %d terms, want 2", len(terms))
	}
	if terms[0].TermCode != "TERM-B" {
		t.Errorf("term code = %q, want TERM-B", terms[0].TermCode)
	}
	if terms[0].PriceDimensions[0].RateCode != "RATE-A" {
		t.Errorf("first rate code = %q, want RATE-A (sorted)", terms[0].PriceDimensions[0].RateCode)
	}
	if terms[1].TermAttributes["PurchaseOption"] != "Partial Upfront" {
		t.Errorf("reserved purchase option = %q", terms[1].TermAttributes["PurchaseOption"])
	}
}

func TestDecodeOfferFileSkipsTermSKUWithoutProduct(t *testing.T) {
	input := `{"products":{},"terms":{"OnDemand":{"SKU-1":{}}}}`
	_, _, err := decodeOfferFile(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("unknown SKU terms should be skipped, got error: %v", err)
	}
}

func TestDecodeOfferFileHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := decodeOfferFile(ctx, strings.NewReader(`{"products":{},"terms":{}}`))
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestDecodeOfferFileRejectsTrailingJSON(t *testing.T) {
	input := `{"products":{},"terms":{}} {"another":"document"}`
	_, _, err := decodeOfferFile(context.Background(), strings.NewReader(input))
	if err == nil {
		t.Fatal("expected trailing JSON to be rejected")
	}
}
