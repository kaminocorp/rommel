# proto/clients/go

Generated Go types for the Rommel wire protocol. Package `protogen`.

- **Source of truth:** `proto/schemas/`
- **Regenerate:** `make proto` from the repo root (or `./proto/codegen/go.sh`)
- **Consumed by:** `sandbox-daemon/` (via a `replace` directive or, once the
  monorepo grows, a `go.work` workspace at the repo root)

`gen/` is gitignored — only this `go.mod` and the README are committed.

The module path uses `rommel-ade` as a placeholder org; swap to the real GitHub
org when the repo is pushed and update consumers' imports in lockstep.
