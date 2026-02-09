.PHONY: lint
lint:
	@CGO_ENABLED=0 go tool -modfile=tools.mod golangci-lint run

.PHONY: test
test:
	@go test -v -race ./...

.PHONY: generate
generate:
	cd examples/grpc && go tool -modfile=../../tools.mod buf generate
