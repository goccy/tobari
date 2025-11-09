.PHONY: lint
lint:
	@go tool golangci-lint run

.PHONY: test
test:
	@go test -v -race ./...
