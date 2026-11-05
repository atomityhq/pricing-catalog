package aws_ebs

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"pricing-catalog/pkg/catalog"
)

const (
	fixtureSKU  = "A-GP3"
	fixtureTerm = "A-GP3.JRTCKXETXF"
	fixtureRate = "A-GP3.JRTCKXETXF.1"
)

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/offer.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Edit raw JSON rather than the source structs, so absent fields and wrong types
// exercise the same decoding path as a real input file.
func editFixture(t *testing.T, path []string, value any, remove bool) []byte {
	t.Helper()
	var offer map[string]any
	if err := json.Unmarshal(readFixture(t), &offer); err != nil {
		t.Fatal(err)
	}
	parent := offer
	for _, key := range path[:len(path)-1] {
		next, ok := parent[key].(map[string]any)
		if !ok {
			t.Fatalf("fixture path %v: %s is not an object", path, key)
		}
		parent = next
	}
	key := path[len(path)-1]
	if remove {
		delete(parent, key)
	} else {
		parent[key] = value
	}
	data, err := json.Marshal(offer)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func requireNormalizeError(t *testing.T, data []byte, want string) {
	t.Helper()
	records, err := normalizeData(data)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("normalizeData error = %v, want containing %q", err, want)
	}
	if records != nil {
		t.Fatalf("invalid input returned %d partial records", len(records))
	}
}

func TestNormalizeDataMapping(t *testing.T) {
	records, err := normalizeData(readFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 8 {
		t.Fatalf("got %d records, want 8", len(records))
	}
	if err := catalog.ValidateRecords(records); err != nil {
		t.Fatalf("canonical records: %v", err)
	}

	effective := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	want := catalog.PricingRecord{
		Provider: "aws", Product: "Amazon EBS", SKU: fixtureSKU,
		ProviderProductID: "AmazonEC2", ProviderSKUID: fixtureSKU,
		Region: "us-east-1", PurchaseModel: "on_demand", BillingUnit: "GB-Mo",
		PricingDimension: "EBS:VolumeUsage.gp3",
		Dimensions: map[string]string{
			"rateCode": fixtureRate, "beginRange": "0", "endRange": "", "unit": "GB-Mo",
			"usagetype": "EBS:VolumeUsage.gp3", "volumeApiName": "gp3",
			"volumeType": "General Purpose", "storageMedia": "SSD-backed",
			"location": "us-east-1", "locationType": "AWS Region",
		},
		Price:         catalog.Money{Amount: "0.0800000000", Currency: "USD"},
		Tier:          &catalog.Tier{StartQuantity: "0", EndQuantity: ""},
		EffectiveFrom: &effective,
		Source: catalog.Source{
			Type: "aws-price-list-json",
			URL:  "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonEC2/20261001000000/us-east-1/index.json",
		},
		Attributes: map[string]string{
			"servicecode": "AmazonEC2", "usagetype": "EBS:VolumeUsage.gp3", "regionCode": "us-east-1",
			"location": "us-east-1", "locationType": "AWS Region", "operation": "",
			"volumeApiName": "gp3", "volumeType": "General Purpose", "storageMedia": "SSD-backed",
			"description": "Fixture price for EBS:VolumeUsage.gp3", "product_family": "Storage",
			"offer_version": "20261001000000",
		},
	}
	if !reflect.DeepEqual(records[0], want) {
		t.Errorf("first record:\n got: %#v\nwant: %#v", records[0], want)
	}

	for i, tier := range []catalog.Tier{
		{StartQuantity: "0", EndQuantity: "32000"},
		{StartQuantity: "32000", EndQuantity: "64000"},
		{StartQuantity: "64000", EndQuantity: ""},
	} {
		record := records[i+1]
		if record.SKU != "B-IOPS" || record.Region != "eu-west-1" ||
			record.BillingUnit != "IOPS-Mo" || !reflect.DeepEqual(record.Tier, &tier) {
			t.Errorf("IOPS tier %d: %#v", i, record)
		}
		if record.Price.Amount != []string{"0.0650000000", "0.0460000000", "0.0320000000"}[i] {
			t.Errorf("IOPS tier %d price: %s", i, record.Price.Amount)
		}
	}
	for i, want := range []struct{ sku, unit, region string }{
		{"C-THROUGHPUT", "MBps-Mo", "us-east-1"},
		{"D-SNAPSHOT", "GB-Mo", "us-east-1"},
		{"E-ARCHIVE", "GB-Mo", "eu-west-1"},
		{"F-RESTORE", "DSU-Hrs", "us-east-1-bos-1"},
	} {
		record := records[i+4]
		if record.SKU != want.sku || record.BillingUnit != want.unit || record.Region != want.region {
			t.Errorf("record %d: %#v", i+4, record)
		}
	}
}

func TestNormalizeDataMalformedJSON(t *testing.T) {
	for _, input := range []string{
		"", "{", `{"products":`, `{"formatVersion":"v1.0",}`, `{"offerCode":AmazonEC2}`,
		"<html>not JSON</html>", "{} {}", "{} trailing data", "[]", "42", `"offer"`,
	} {
		t.Run(input, func(t *testing.T) {
			requireNormalizeError(t, []byte(input), "decode AWS offer JSON")
		})
	}
	for _, input := range []string{"null", "{}"} {
		t.Run(input, func(t *testing.T) {
			requireNormalizeError(t, []byte(input), "unsupported format version")
		})
	}
}

func TestNormalizeDataMissingFields(t *testing.T) {
	cases := []struct {
		path []string
		want string
	}{
		{[]string{"formatVersion"}, "unsupported format version"},
		{[]string{"offerCode"}, "unsupported offer code"},
		{[]string{"version"}, "version is required"},
		{[]string{"products"}, "products must not be empty"},
		{[]string{"terms"}, "terms is required"},
		{[]string{"terms", "OnDemand"}, "terms.OnDemand is required"},
		{[]string{"products", fixtureSKU, "sku"}, "invalid product SKU"},
		{[]string{"products", fixtureSKU, "productFamily"}, "productFamily is required"},
		{[]string{"products", fixtureSKU, "attributes"}, "attributes.usagetype is required"},
		{[]string{"products", fixtureSKU, "attributes", "usagetype"}, "attributes.usagetype is required"},
		{[]string{"products", fixtureSKU, "attributes", "servicecode"}, "attributes.servicecode must be AmazonEC2"},
		{[]string{"products", fixtureSKU, "attributes", "regionCode"}, "attributes.regionCode is required"},
		{[]string{"terms", "OnDemand", fixtureSKU}, "no OnDemand terms found"},
		{[]string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "sku"}, "invalid EBS term"},
		{[]string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "offerTermCode"}, "invalid EBS term"},
		{[]string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "effectiveDate"}, "invalid effectiveDate"},
		{[]string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "priceDimensions"}, "invalid EBS term"},
	}
	for field, want := range map[string]string{
		"rateCode": "invalid rateCode", "description": "description is required", "unit": "unit is required",
		"beginRange": "invalid beginRange", "endRange": "invalid endRange", "pricePerUnit": "pricePerUnit must not be empty",
	} {
		cases = append(cases, struct {
			path []string
			want string
		}{[]string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "priceDimensions", fixtureRate, field}, want})
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.path, "/"), func(t *testing.T) {
			t.Run("absent", func(t *testing.T) {
				requireNormalizeError(t, editFixture(t, tc.path, nil, true), tc.want)
			})
			t.Run("null", func(t *testing.T) {
				requireNormalizeError(t, editFixture(t, tc.path, nil, false), tc.want)
			})
		})
	}
}

func TestNormalizeDataWrongTypes(t *testing.T) {
	for _, path := range [][]string{
		{"formatVersion"}, {"products"}, {"terms"},
		{"products", fixtureSKU, "sku"}, {"products", fixtureSKU, "attributes"},
		{"products", fixtureSKU, "attributes", "regionCode"}, {"terms", "OnDemand"},
		{"terms", "OnDemand", fixtureSKU, fixtureTerm, "effectiveDate"},
		{"terms", "OnDemand", fixtureSKU, fixtureTerm, "priceDimensions"},
		{"terms", "OnDemand", fixtureSKU, fixtureTerm, "priceDimensions", fixtureRate, "beginRange"},
		{"terms", "OnDemand", fixtureSKU, fixtureTerm, "priceDimensions", fixtureRate, "unit"},
		{"terms", "OnDemand", fixtureSKU, fixtureTerm, "priceDimensions", fixtureRate, "pricePerUnit", "USD"},
		{"terms", "OnDemand", fixtureSKU, fixtureTerm, "priceDimensions", fixtureRate, "appliesTo"},
	} {
		t.Run(strings.Join(path, "/"), func(t *testing.T) {
			requireNormalizeError(t, editFixture(t, path, 123, false), "decode AWS offer JSON")
		})
	}
}

func TestNormalizeDataInvalidValues(t *testing.T) {
	pricePath := []string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "priceDimensions", fixtureRate}
	cases := []struct {
		name, want string
		path       []string
		value      any
	}{
		{"format", "unsupported format version", []string{"formatVersion"}, "v2.0"},
		{"service", "unsupported offer code", []string{"offerCode"}, "AWSLambda"},
		{"empty version", "version is required", []string{"version"}, " "},
		{"empty products", "products must not be empty", []string{"products"}, map[string]any{}},
		{"empty terms", "terms.OnDemand is required", []string{"terms"}, map[string]any{}},
		{"mismatched SKU", "invalid product SKU", []string{"products", fixtureSKU, "sku"}, "OTHER"},
		{"wrong product service", "servicecode must be AmazonEC2", []string{"products", fixtureSKU, "attributes", "servicecode"}, "AmazonS3"},
		{"empty region", "regionCode is required", []string{"products", fixtureSKU, "attributes", "regionCode"}, " "},
		{"empty OnDemand", "no OnDemand terms", []string{"terms", "OnDemand", fixtureSKU}, map[string]any{}},
		{"Reserved EBS", "unsupported EBS purchase model", []string{"terms", "Reserved", fixtureSKU}, map[string]any{}},
		{"term SKU mismatch", "invalid EBS term", []string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "sku"}, "OTHER"},
		{"term key mismatch", "invalid EBS term", []string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "offerTermCode"}, "OTHER"},
		{"bad date", "invalid effectiveDate", []string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "effectiveDate"}, "2026-02-30T00:00:00Z"},
		{"date without zone", "invalid effectiveDate", []string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "effectiveDate"}, "2026-10-01"},
		{"empty dimensions", "invalid EBS term", []string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "priceDimensions"}, map[string]any{}},
	}
	for field, value := range map[string]any{
		"rateCode": "OTHER", "description": " ", "unit": " ", "pricePerUnit": map[string]any{},
	} {
		want := map[string]string{"rateCode": "invalid rateCode", "description": "description is required", "unit": "unit is required", "pricePerUnit": "pricePerUnit must not be empty"}[field]
		cases = append(cases, struct {
			name, want string
			path       []string
			value      any
		}{field, want, append(append([]string{}, pricePath...), field), value})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireNormalizeError(t, editFixture(t, tc.path, tc.value, false), tc.want)
		})
	}
	for _, field := range []string{"beginRange", "endRange"} {
		for _, value := range []string{"", "-1", "NaN", "Infinity", "1e3", "1/2", "01", " 1", "+1"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				requireNormalizeError(t, editFixture(t, append(append([]string{}, pricePath...), field), value, false), "invalid "+field)
			})
		}
	}
	for _, amount := range []any{"", nil, "-0.01", "NaN", "Inf", "1e-3", "1/2", "01", " 0.1", "+1"} {
		t.Run("invalid amount", func(t *testing.T) {
			path := append(append([]string{}, pricePath...), "pricePerUnit", "USD")
			requireNormalizeError(t, editFixture(t, path, amount, false), "invalid price amount")
		})
	}
	for _, currencies := range []map[string]string{{"EUR": "0.1"}, {"usd": "0.1"}, {"USD": "0.1", "EUR": "0.2"}} {
		t.Run("currency", func(t *testing.T) {
			requireNormalizeError(t, editFixture(t, append(pricePath, "pricePerUnit"), currencies, false), "unsupported currency")
		})
	}
}

func TestNormalizeDataRangesAndPrecision(t *testing.T) {
	for _, tc := range []struct {
		begin, end, amount string
		valid              bool
	}{
		{"0", "Inf", "0.0000000000", true},
		{"0.1", "0.2", "0.123456789012345678901234567890", true},
		{"100000000000000000000000000000000000000", "100000000000000000000000000000000000001", "0.1", true},
		{"1", "1", "0.1", false}, {"2", "1", "0.1", false},
		{"Inf", "Inf", "0.1", false}, {"-1", "Inf", "0.1", false},
	} {
		t.Run(tc.begin+"/"+tc.end, func(t *testing.T) {
			price := map[string]any{
				"rateCode": fixtureRate, "description": "Test tier", "unit": "GB-Mo",
				"beginRange": tc.begin, "endRange": tc.end, "pricePerUnit": map[string]string{"USD": tc.amount},
			}
			data := editFixture(t, []string{"terms", "OnDemand", fixtureSKU, fixtureTerm, "priceDimensions", fixtureRate}, price, false)
			if !tc.valid {
				requireNormalizeError(t, data, "invalid")
				return
			}
			records, err := normalizeData(data)
			if err != nil {
				t.Fatal(err)
			}
			end := tc.end
			if end == "Inf" {
				end = ""
			}
			if records[0].Price.Amount != tc.amount || records[0].Tier.StartQuantity != tc.begin || records[0].Tier.EndQuantity != end {
				t.Fatalf("price or tier lost precision: %#v", records[0])
			}
		})
	}
}

func TestNormalizeDataMetadataAndOptionalFields(t *testing.T) {
	term := map[string]any{
		"sku": fixtureSKU, "offerTermCode": "JRTCKXETXF", "effectiveDate": "2026-10-01T00:00:00Z",
		"termAttributes": map[string]string{"rateCode": "preserve without overwriting"},
		"priceDimensions": map[string]any{fixtureRate: map[string]any{
			"rateCode": fixtureRate, "description": "Conditional price", "unit": "GB-Mo",
			"beginRange": "0", "endRange": "Inf", "pricePerUnit": map[string]string{"USD": "0.1"},
			"appliesTo": []string{"SKU-ONE", "SKU-TWO"},
		}},
	}
	data := editFixture(t, []string{"terms", "OnDemand", fixtureSKU, fixtureTerm}, term, false)
	records, err := normalizeData(data)
	if err != nil {
		t.Fatal(err)
	}
	dims := records[0].Dimensions
	if dims["rateCode"] != fixtureRate || dims["term_attribute.rateCode"] != "preserve without overwriting" || dims["applies_to"] != `["SKU-ONE","SKU-TWO"]` {
		t.Fatalf("lost metadata: %#v", dims)
	}
	for _, path := range [][]string{
		{"terms", "OnDemand", fixtureSKU, fixtureTerm, "termAttributes"},
		{"terms", "OnDemand", fixtureSKU, fixtureTerm, "priceDimensions", fixtureRate, "appliesTo"},
		{"products", fixtureSKU, "attributes", "volumeApiName"},
	} {
		if _, err := normalizeData(editFixture(t, path, nil, true)); err != nil {
			t.Errorf("optional field %v: %v", path, err)
		}
	}
	if _, err := normalizeData(editFixture(t, []string{"publicationDate"}, "2026-10-01T00:00:00Z", false)); err != nil {
		t.Errorf("additional AWS fields: %v", err)
	}
}

func TestNormalizeDataDeterministicAndIndependent(t *testing.T) {
	data := readFixture(t)
	want, err := normalizeData(data)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		got, err := normalizeData(data)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d is not deterministic: %v", i, err)
		}
	}
	want[1].Attributes["regionCode"] = "changed"
	want[1].Dimensions["unit"] = "changed"
	*want[1].EffectiveFrom = time.Time{}
	if want[2].Attributes["regionCode"] != "eu-west-1" || want[2].Dimensions["unit"] != "IOPS-Mo" || want[2].EffectiveFrom.IsZero() {
		t.Fatal("records share mutable maps or effective dates")
	}
}

func TestNormalizeDataNoEBSProducts(t *testing.T) {
	products := map[string]any{"COMPUTE": map[string]any{
		"sku": "COMPUTE", "productFamily": "Compute Instance",
		"attributes": map[string]string{"usagetype": "BoxUsage:t3.micro"},
	}}
	records, err := normalizeData(editFixture(t, []string{"products"}, products, false))
	if err != nil || len(records) != 0 {
		t.Fatalf("no EBS products: records = %v, error = %v", records, err)
	}
}
