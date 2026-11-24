.PHONY: run build test race cover vet fmt docker

run:
	go run ./cmd/storefront

build:
	go build -o bin/storefront ./cmd/storefront

test:
	go test ./...

race:
	go test -race ./...

# Same measurement as CI: code exercised from any package's tests counts.
cover:
	rm -rf coverage && mkdir coverage
	go test -cover -coverpkg=./internal/... ./... -args -test.gocoverdir="$(CURDIR)/coverage"
	go tool covdata textfmt -i=coverage -o coverage.out
	go tool cover -func=coverage.out

vet:
	go vet ./...

fmt:
	gofmt -w .

docker:
	docker build -t storefront-api .
