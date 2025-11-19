.PHONY: lint
lint:
	@go tool -modfile=tools.mod golangci-lint run

.PHONY: test
test:
	@go test -v -race ./...
