# Rommel — top-level router.
# Each target delegates into the relevant subdir; this Makefile does no real work itself.
# Subtrees are added incrementally as they're scaffolded; targets that reference a missing
# subtree no-op with a friendly note rather than failing the whole pipeline.

.PHONY: help bootstrap proto dev lint test build clean
.DEFAULT_GOAL := help

# --- helpers ----------------------------------------------------------------

# Run a command in a subdir if the subdir exists; otherwise skip with a note.
define run_if_exists
	@if [ -d "$(1)" ]; then \
		echo ">>> $(2) ($(1))"; \
		$(MAKE) -C $(1) $(3); \
	else \
		echo "--- skip $(2): $(1)/ not yet scaffolded"; \
	fi
endef

help:
	@echo "Rommel monorepo — top-level targets:"
	@echo "  make bootstrap   install toolchain deps across all subtrees"
	@echo "  make proto       regenerate TS / Go / Pydantic clients from proto/schemas"
	@echo "  make dev         run frontend + backend + local daemon concurrently"
	@echo "  make lint        run all linters"
	@echo "  make test        run all tests"
	@echo "  make build       build everything (no deploy)"
	@echo "  make clean       remove build artifacts across subtrees"

# --- bootstrap --------------------------------------------------------------

bootstrap:
	@echo ">>> bootstrap: installing pnpm workspace deps"
	@if command -v pnpm >/dev/null 2>&1; then pnpm install; else echo "pnpm not found; install via corepack or npm i -g pnpm"; fi
	$(call run_if_exists,backend,bootstrap,bootstrap)
	$(call run_if_exists,sandbox-daemon,bootstrap,bootstrap)

# --- proto ------------------------------------------------------------------

proto:
	@if [ -x proto/codegen.sh ]; then \
		./proto/codegen.sh; \
	else \
		echo "--- skip proto: proto/codegen.sh not yet present"; \
	fi

# --- dev / lint / test / build ---------------------------------------------

dev:
	@echo "TODO: spawn frontend, backend, and a local daemon concurrently."
	@echo "      Implement once at least two subtrees exist."

lint:
	$(call run_if_exists,frontend,lint,lint)
	$(call run_if_exists,backend,lint,lint)
	$(call run_if_exists,sandbox-daemon,lint,lint)

test:
	$(call run_if_exists,frontend,test,test)
	$(call run_if_exists,backend,test,test)
	$(call run_if_exists,sandbox-daemon,test,test)

build:
	$(call run_if_exists,frontend,build,build)
	$(call run_if_exists,backend,build,build)
	$(call run_if_exists,sandbox-daemon,build,build)
	$(call run_if_exists,workspace-image,build,build)

clean:
	$(call run_if_exists,frontend,clean,clean)
	$(call run_if_exists,backend,clean,clean)
	$(call run_if_exists,sandbox-daemon,clean,clean)
	$(call run_if_exists,workspace-image,clean,clean)
	@rm -rf node_modules
