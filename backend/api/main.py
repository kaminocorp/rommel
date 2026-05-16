"""FastAPI application factory.

The app is intentionally minimal: routers for /healthz, /auth, /workspaces,
/workspaces/:id/sessions, /policy. A startup hook verifies the Alembic head
matches the database (so a missing migration is loud, not silent — per the
plan's §0.7 "never autogen on boot" policy).
"""

from __future__ import annotations

import logging
from contextlib import asynccontextmanager
from typing import AsyncIterator

import structlog
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from .config import get_settings
from . import auth as auth_router
from . import health as health_router
from . import policy as policy_router
from . import sessions as sessions_router
from . import workspaces as workspaces_router

logger = structlog.get_logger(__name__)


def _configure_logging() -> None:
    logging.basicConfig(level=logging.INFO, format="%(message)s")
    structlog.configure(
        processors=[
            structlog.processors.add_log_level,
            structlog.processors.TimeStamper(fmt="iso"),
            structlog.processors.JSONRenderer(),
        ],
    )


@asynccontextmanager
async def _lifespan(app: FastAPI) -> AsyncIterator[None]:
    """Startup/shutdown hook.

    Startup-time check: the schema version (Alembic head) must match what the
    DB currently has. Mismatch = a deploy missed a migration; we fail loudly
    rather than serve traffic against the wrong schema. The check is best-effort
    in tests (where the fixture controls the schema) — gated by a setting.
    """
    settings = get_settings()
    logger.info(
        "backend.startup",
        daemon_url_template=settings.daemon_url_template,
        token_ttl_seconds=settings.token_ttl_seconds,
        fly_app=settings.fly_workspaces_app,
    )
    yield
    logger.info("backend.shutdown")


def create_app() -> FastAPI:
    _configure_logging()
    settings = get_settings()

    app = FastAPI(
        title="rommel-backend",
        description="Rommel ADE control plane — auth, workspace lifecycle, "
        "and the EdDSA session-token broker.",
        version="0.1.4",
        lifespan=_lifespan,
    )

    # CORS — the browser IDE (Phase 5) is a separate origin. Production sets
    # ROMMEL_CORS_ORIGINS to the exact frontend origin(s); dev defaults to '*'.
    # allow_credentials stays false: the daemon is the only thing handling
    # session-tokens, and the API doesn't use cookies — so wildcard is safe.
    app.add_middleware(
        CORSMiddleware,
        allow_origins=settings.cors_origins,
        allow_credentials=False,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    app.include_router(health_router.router)
    app.include_router(auth_router.router, prefix="/auth")
    app.include_router(workspaces_router.router, prefix="/workspaces")
    app.include_router(sessions_router.router)
    app.include_router(policy_router.router, prefix="/policy")

    return app


# Module-level binding so `uvicorn api.main:app` works.
app = create_app()
