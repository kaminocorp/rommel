# frontend/ — Next.js 15 IDE shell. Root Makefile calls these via run_if_exists.

.PHONY: bootstrap dev lint test build clean

bootstrap:
	pnpm install --filter ./frontend...

dev:
	pnpm --filter ./frontend dev

lint:
	pnpm --filter ./frontend lint

test:
	pnpm --filter ./frontend test:unit

build:
	pnpm --filter ./frontend build

clean:
	pnpm --filter ./frontend clean
