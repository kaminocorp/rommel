.PHONY: help bootstrap lint test build run-local clean proto-go

BIN := dist/sandbox-daemon
PKG := ./...

# The Go proto client lives in a sibling module and is gitignored (regenerated
# from proto/schemas/). Every daemon target that compiles code depends on it.
PROTOGEN := ../proto/clients/go/gen/proto.go

help:
	@echo "sandbox-daemon — targets:"
	@echo "  make bootstrap   regen proto Go client + go mod download"
	@echo "  make lint        go vet (+ golangci-lint if installed)"
	@echo "  make test        go test ./..."
	@echo "  make build       static binary at $(BIN)"
	@echo "  make run-local   run from source with dev env (see README.md)"
	@echo "  make clean       remove dist/"

$(PROTOGEN):
	@echo ">>> regenerating Go proto client (gitignored)"
	bash ../proto/codegen/go.sh

proto-go: $(PROTOGEN)

bootstrap: $(PROTOGEN)
	go mod download

lint: $(PROTOGEN)
	go vet $(PKG)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed; skipping (go vet ran)"; \
	fi

test: $(PROTOGEN)
	go test $(PKG)

build: $(PROTOGEN)
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/daemon

run-local:
	@if [ -z "$$ROMMEL_WORKSPACE_ROOT" ]; then echo "set ROMMEL_WORKSPACE_ROOT (see README)"; exit 1; fi
	@if [ -z "$$ROMMEL_WID" ]; then echo "set ROMMEL_WID"; exit 1; fi
	@if [ -z "$$ROMMEL_TOKEN_PUBKEY" ]; then echo "set ROMMEL_TOKEN_PUBKEY (PEM)"; exit 1; fi
	go run ./cmd/daemon

clean:
	rm -rf dist
