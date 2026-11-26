.PHONY: check test vet build

check: vet test

test:
	go test -cover ./...

vet:
	go vet ./...
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "files above need gofmt"; exit 1; }

build:
	go build -o bin/ ./cmd/flowtocsv
