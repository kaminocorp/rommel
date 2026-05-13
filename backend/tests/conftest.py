"""Shared pytest fixtures.

Two groups:
  - hermetic fixtures (no external deps) — used by health/auth/sessions unit tests
  - daemon fixture (spawns the Go binary on a free port) — used by the
    integration-gate test in test_sessions.py

Postgres-backed tests are guarded by `require_postgres` and skip cleanly if
no DB is reachable. CI runs them against a service container; local devs run
them via `docker compose up postgres` before `pytest`.
"""

from __future__ import annotations

import os
import socket
import subprocess
import sys
import time
import uuid
from collections.abc import Iterator
from pathlib import Path

import httpx
import pytest
import pytest_asyncio
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from fastapi import FastAPI
from httpx import ASGITransport, AsyncClient

# Ensure `backend/` is on sys.path so `from api...` resolves when pytest runs
# from the repo root (e.g. `pytest backend/tests`).
_BACKEND_ROOT = Path(__file__).resolve().parent.parent
if str(_BACKEND_ROOT) not in sys.path:
    sys.path.insert(0, str(_BACKEND_ROOT))


# --- crypto -----------------------------------------------------------------


@pytest.fixture(scope="session")
def ed25519_keypair() -> tuple[str, str]:
    """Generate a fresh Ed25519 keypair for the test session.

    Returns (private_pem, public_pem) as strings — matches the env-var
    convention on both backend (`ROMMEL_TOKEN_PRIVKEY`) and daemon
    (`ROMMEL_TOKEN_PUBKEY`).
    """
    priv = Ed25519PrivateKey.generate()
    priv_pem = priv.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    ).decode("utf-8")
    pub_pem = priv.public_key().public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    ).decode("utf-8")
    return priv_pem, pub_pem


# --- settings ---------------------------------------------------------------


@pytest.fixture()
def test_settings(ed25519_keypair: tuple[str, str], monkeypatch: pytest.MonkeyPatch):
    """Build a Settings instance overridden for tests.

    All env-driven config is replaced with deterministic values. Tests that
    need a real daemon URL get it from the `daemon_subprocess` fixture and
    further patch `daemon_url_template`.
    """
    priv_pem, _ = ed25519_keypair
    # Clear the cached get_settings() singleton so we can re-instantiate.
    from api.config import Settings, get_settings

    get_settings.cache_clear()

    monkeypatch.setenv("ROMMEL_DATABASE_URL", "postgresql+asyncpg://localhost/none")
    monkeypatch.setenv("ROMMEL_DATABASE_MIGRATE_URL", "postgresql://localhost/none")
    monkeypatch.setenv("ROMMEL_TOKEN_PRIVKEY", priv_pem)
    monkeypatch.setenv("ROMMEL_DAEMON_URL_TEMPLATE", "ws://localhost:7777/ws")
    monkeypatch.setenv("ROMMEL_SUPABASE_JWKS_URL", "https://example.invalid/jwks.json")
    monkeypatch.setenv("ROMMEL_FLY_API_TOKEN", "")  # dev-stub mode
    monkeypatch.setenv("ROMMEL_DEFAULT_SCOPES", "fs:rw,pty:rw")

    s = Settings()  # type: ignore[call-arg]
    yield s
    get_settings.cache_clear()


# --- FastAPI app + client ---------------------------------------------------


@pytest.fixture()
def app(test_settings) -> FastAPI:
    """Build a FastAPI app with Settings + the auth dep overridden.

    The auth override pins a fake user so unit tests don't need a Supabase
    JWT. Integration-gate tests still test the broker→daemon path directly.
    """
    from api.config import get_settings
    from api.deps import get_current_user, get_db, get_db_for_user
    from api.main import create_app
    from services.auth import UserClaims

    application = create_app()

    application.dependency_overrides[get_settings] = lambda: test_settings

    fake_user = UserClaims(sub="test-user-sub", email="test@example.com")
    application.dependency_overrides[get_current_user] = lambda: fake_user

    # Bypass DB entirely for unit tests — the routes that need DB are tested
    # via direct repository calls or under `require_postgres`.
    async def _no_db() -> None:
        yield None

    application.dependency_overrides[get_db] = _no_db
    application.dependency_overrides[get_db_for_user] = _no_db

    return application


@pytest_asyncio.fixture()
async def client(app: FastAPI) -> AsyncClient:
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as ac:
        yield ac


# --- daemon subprocess (integration gate) -----------------------------------


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _daemon_binary_path() -> Path:
    # `sandbox-daemon/dist/sandbox-daemon` per the daemon's Makefile.
    return _BACKEND_ROOT.parent / "sandbox-daemon" / "dist" / "sandbox-daemon"


@pytest.fixture(scope="session")
def daemon_binary() -> Path:
    """Build the sandbox-daemon binary if not already built. Skips the test
    if Go isn't installed (local devs without a Go toolchain can still run
    the unit-test suite).
    """
    binary = _daemon_binary_path()
    if not binary.exists():
        if not _have("go"):
            pytest.skip("go toolchain not available — skipping daemon integration gate")
        repo_root = _BACKEND_ROOT.parent
        subprocess.run(  # noqa: S603,S607
            ["make", "-C", "sandbox-daemon", "build"],
            cwd=repo_root,
            check=True,
        )
    if not binary.exists():
        pytest.skip(f"daemon binary missing at {binary} after build")
    return binary


def _have(cmd: str) -> bool:
    return subprocess.run(  # noqa: S603,S607
        ["which", cmd], capture_output=True
    ).returncode == 0


@pytest.fixture()
def daemon_subprocess(
    tmp_path: Path,
    ed25519_keypair: tuple[str, str],
    daemon_binary: Path,
) -> Iterator[dict[str, str]]:
    """Spawn the daemon on a free port with the matching pubkey, then
    tear down on exit. Yields a dict with `wid`, `port`, `ws_url`.
    """
    _, pub_pem = ed25519_keypair
    port = _free_port()
    wid = f"ws-test-{uuid.uuid4().hex[:8]}"

    env = {
        **os.environ,
        "ROMMEL_PORT": str(port),
        "ROMMEL_WORKSPACE_ROOT": str(tmp_path),
        "ROMMEL_WID": wid,
        "ROMMEL_TOKEN_PUBKEY": pub_pem,
    }
    proc = subprocess.Popen(  # noqa: S603
        [str(daemon_binary)],
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )

    # Poll /healthz until it answers (or give up after a few seconds).
    deadline = time.time() + 10
    while time.time() < deadline:
        try:
            r = httpx.get(f"http://127.0.0.1:{port}/healthz", timeout=0.5)
            if r.status_code == 200:
                break
        except httpx.HTTPError:
            time.sleep(0.1)
    else:
        proc.terminate()
        out = proc.stdout.read().decode("utf-8", errors="replace") if proc.stdout else ""
        pytest.fail(f"daemon failed to come up on :{port}\n--- daemon log ---\n{out}")

    yield {"wid": wid, "port": str(port), "ws_url": f"ws://127.0.0.1:{port}/ws"}

    proc.terminate()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()


# --- Postgres gate (skip if not reachable) ---------------------------------


def _postgres_reachable(url: str) -> bool:
    # Quick TCP probe — avoids importing asyncpg at module level.
    import urllib.parse

    parsed = urllib.parse.urlparse(url.replace("postgresql+asyncpg", "postgresql"))
    host = parsed.hostname or "localhost"
    port = parsed.port or 5432
    try:
        with socket.create_connection((host, port), timeout=0.5):
            return True
    except OSError:
        return False


@pytest.fixture()
def require_postgres() -> str:
    url = os.environ.get(
        "ROMMEL_DATABASE_URL",
        "postgresql+asyncpg://rommel:rommel@localhost:5432/rommel",
    )
    if not _postgres_reachable(url):
        pytest.skip("Postgres not reachable — `docker compose up postgres` to run DB tests")
    return url
