.PHONY: run build test race cover vet fmt docker

run:
	go run ./cmd/storefront

build:
	go build -o bin/storefront ./cmd/storefront

test:
	go test ./...

race:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

vet:
	go vet ./...

fmt:
	gofmt -w .

docker:
	docker build -t storefront-api .
