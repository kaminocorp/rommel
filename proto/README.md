# proto/ — protocol source-of-truth

Single source of truth for every type that crosses a process boundary in Rommel.
Schemas are JSON Schema (draft 2020-12); clients in TypeScript, Go, and Python
are **generated** from `schemas/` — never hand-edited.

## Layout

```
proto/
├── README.md
├── codegen.sh                    # runs all three codegen scripts
├── schemas/
│   ├── envelope.json             # request / response / event envelope
│   ├── session-token.json        # JWT claims minted by backend, validated by daemon
│   ├── fs/                       # filesystem primitives
│   │   ├── read.json
│   │   ├── write.json
│   │   ├── list.json
│   │   └── watch-event.json
│   ├── pty/                      # PTY primitives
│   │   ├── open.json
│   │   ├── input.json
│   │   ├── output-event.json
│   │   └── resize.json
│   ├── git/                      # placeholder; one file per primitive
│   ├── funnel/                   # placeholder; one file per primitive
│   └── workspace/
│       └── info.json
├── codegen/                      # codegen tooling (per-language scripts)
│   ├── ts.sh
│   ├── go.sh
│   └── python.sh
└── clients/                      # generated outputs + per-client packaging
    ├── ts/                       # @rommel/proto — pnpm workspace dep
    ├── go/                       # github.com/.../proto/clients/go — Go module
    └── python/                   # rommel_proto — Python package
```

Generated source under `clients/*/{src,gen}/` is **gitignored**. Only the
package metadata (`package.json`, `go.mod`, `pyproject.toml`, an `index` entry
point) is committed. CI re-runs codegen and fails if the result diverges from
the committed schemas.

## Format choice — why JSON Schema (not Protobuf)

`techstack.md` left this open. Picked JSON Schema for v1:

- Daemon traffic is JSON-over-WebSocket per `primitives.md` cross-cutting Q1
  — no binary framing layer to bolt on.
- Browser devtools render the wire format directly (no `.proto` decoder needed
  to read a frame in the Network panel — this is huge for hot-path debugging).
- Mature codegen on all three sides:
  - TS: `json-schema-to-typescript` (`json2ts`)
  - Go: `omissis/go-jsonschema`
  - Python: `datamodel-code-generator` (Pydantic v2)
- Switch to Protobuf later if profiling demands it; the schemas port over
  field-for-field.

## How to add a new schema

1. Drop a new `*.json` file under `schemas/<domain>/`.
2. Use draft 2020-12: `"$schema": "https://json-schema.org/draft/2020-12/schema"`.
3. Set `"$id"` to a stable URI: `https://rommel.dev/schemas/<domain>/<name>.json`.
4. Set `"title"` — codegen uses it as the type name (PascalCase, no spaces).
5. Run `make proto` from the repo root.
6. Commit the schema; do **not** commit the generated client source.

## How to regenerate clients

```sh
make proto                # from repo root — runs all three
./proto/codegen.sh        # equivalent
./proto/codegen/ts.sh     # one language only
```

The codegen scripts are idempotent: running twice produces zero diff.

## Tooling install (first run)

- TS / Go: `npx` and `go run` fetch their tools on demand. Nothing to install.
- Python: the script creates a self-contained venv at `proto/codegen/.venv`
  on first run, installs `datamodel-code-generator` into it, and reuses it
  thereafter. Add `proto/codegen/.venv` is gitignored.
