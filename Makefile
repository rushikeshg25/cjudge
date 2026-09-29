.PHONY: build test race vet fmt integration
VERSION ?= 1.0.0
build:
	mkdir -p bin
	go build -trimpath -ldflags="-X main.version=$(VERSION)" -o bin/cjudge ./cmd/cjudge
test:
	go test ./...
race:
	go test -race ./...
vet:
	go vet ./...
fmt:
	gofmt -w $$(find cmd internal -name '*.go')
integration:
	go test -tags=integration -count=1 ./internal/store
