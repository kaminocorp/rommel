# rommel-backend — Fly app `rommel-backend`. Built and pushed from the
# `backend/` subtree (build context = subtree root, NOT repo root — unlike the
# workspace-image, this build doesn't need to see the rest of the monorepo).

FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    POETRY_VIRTUALENVS_CREATE=false \
    POETRY_VERSION=1.8.3 \
    POETRY_NO_INTERACTION=1

# `curl` is used by Fly's health checks; everything else is unneeded.
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir "poetry==${POETRY_VERSION}"

WORKDIR /app

# Layer the dep install so app source changes don't bust the dep cache.
COPY pyproject.toml poetry.lock* ./
RUN poetry install --no-root --only main

COPY . .

# Migrations run as the Fly `release_command` (see fly.toml), not on app boot.
# This avoids the multi-replica boot-race classic foot-gun (plan §0.7).
EXPOSE 8080
CMD ["uvicorn", "api.main:app", "--host", "0.0.0.0", "--port", "8080"]
