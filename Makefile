TOBARI_BIN := $(CURDIR)/.tobari-dev

.PHONY: tidy
tidy:
	go mod tidy
	go mod tidy -modfile=tools.mod

.PHONY: lint
lint:
	@CGO_ENABLED=0 go tool -modfile=tools.mod golangci-lint run

.PHONY: test
test:
	@go test -v -race ./...

.PHONY: generate
generate:
	cd examples/grpc && go tool -modfile=../../tools.mod buf generate

$(TOBARI_BIN):
	go build -o $@ ./cmd/tobari

.PHONY: kobuild
kobuild: $(TOBARI_BIN)
	cd testdata/notobari && GOFLAGS="$$($(TOBARI_BIN) flags -E)" KO_DOCKER_REPO=ko.local go tool -modfile=../../tools.mod ko build --bare --sbom=none --tags latest .
