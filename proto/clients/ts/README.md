# @rommel/proto

Generated TypeScript types for the Rommel wire protocol.

- **Source of truth:** `proto/schemas/`
- **Regenerate:** `make proto` from the repo root (or `./proto/codegen/ts.sh`)
- **Consumed by:** `frontend/` as a pnpm workspace dep (`@rommel/proto`)

`src/` is gitignored — only this `package.json` is committed. CI fails the
build if regenerating produces a diff against `schemas/`.
