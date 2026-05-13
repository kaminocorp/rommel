.PHONY: help bootstrap run lint test build migrate migrate-new clean deploy

PY ?= poetry run
PORT ?= 8080

help:
	@echo "rommel-backend — targets:"
	@echo "  make bootstrap     poetry install (creates .venv on first run)"
	@echo "  make run           uvicorn api.main:app --reload --port $(PORT)"
	@echo "  make lint          ruff check ."
	@echo "  make test          pytest"
	@echo "  make build         no-op (Docker handles packaging; kept for top-level parity)"
	@echo "  make migrate       alembic upgrade head"
	@echo "  make migrate-new   alembic revision --autogenerate -m '<msg>' (set MSG=...)"
	@echo "  make clean         remove caches / .venv"
	@echo "  make deploy        fly deploy"

bootstrap:
	@command -v poetry >/dev/null 2>&1 || { echo "poetry not found; install via 'pipx install poetry'"; exit 1; }
	poetry install --no-interaction

run:
	$(PY) uvicorn api.main:app --host 0.0.0.0 --port $(PORT) --reload

lint:
	$(PY) ruff check .
	$(PY) ruff format --check .

test:
	$(PY) pytest

# `build` exists for parity with sibling subtrees' Makefiles (sandbox-daemon,
# workspace-image). Backend packaging is handled by the Dockerfile; no separate
# build step.
build:
	@echo "backend: build is a no-op; image is built via Dockerfile"

migrate:
	$(PY) alembic upgrade head

migrate-new:
	@if [ -z "$(MSG)" ]; then echo 'set MSG="describe migration"'; exit 1; fi
	$(PY) alembic revision --autogenerate -m "$(MSG)"

clean:
	rm -rf .venv .pytest_cache .ruff_cache __pycache__ .mypy_cache
	find . -type d -name __pycache__ -exec rm -rf {} +

deploy:
	@command -v fly >/dev/null 2>&1 || { echo "flyctl not found; install via 'brew install flyctl'"; exit 1; }
	fly deploy --remote-only
