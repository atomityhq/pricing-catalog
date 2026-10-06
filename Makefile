.PHONY: fmt test vet check validate-example

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

test:
	go test ./...

vet:
	go vet ./...

check: vet test
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"

validate-example:
	go run ./cmd/pricing-catalog validate ./pkg/catalog/data/example.json
