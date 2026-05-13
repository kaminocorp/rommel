.PHONY: help build push run-local clean

IMAGE ?= rommel-workspaces
TAG   ?= $(shell git rev-parse --short HEAD)

help:
	@echo "workspace-image — targets:"
	@echo "  make build         docker build $(IMAGE):$(TAG) from repo root"
	@echo "  make push          tag + push to registry.fly.io (needs flyctl + fly auth)"
	@echo "  make run-local     run the image locally on :7777 (needs ROMMEL_WID)"
	@echo "  make clean         remove local image tags"

build:
	IMAGE=$(IMAGE) TAG=$(TAG) bash scripts/build.sh

push:
	IMAGE=$(IMAGE) TAG=$(TAG) bash scripts/push.sh

# Smoke test: requires a matching keypair so the daemon can validate any
# token a developer mints. See README.md "Local smoke test" for the
# end-to-end recipe.
run-local: build
	@if [ -z "$$ROMMEL_WID" ]; then echo "set ROMMEL_WID for local smoke test"; exit 1; fi
	docker run --rm -p 7777:7777 \
	  -e ROMMEL_WID="$$ROMMEL_WID" \
	  $(IMAGE):$(TAG)

clean:
	-docker image rm $(IMAGE):$(TAG) 2>/dev/null || true
	-docker image rm $(IMAGE):latest 2>/dev/null || true
