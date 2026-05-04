# rommel-proto (Python)

Generated Pydantic v2 models for the Rommel wire protocol.

- **Source of truth:** `proto/schemas/`
- **Regenerate:** `make proto` from the repo root (or `./proto/codegen/python.sh`)
- **Consumed by:** `backend/` as a path dep in its `pyproject.toml`

`gen/` is gitignored — only this `pyproject.toml` is committed. The codegen
script uses a self-contained venv at `proto/codegen/.venv/` so no global
Python deps are needed.
