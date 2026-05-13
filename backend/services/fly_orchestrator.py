"""Thin httpx client over the Fly Machines API.

v1 surface is intentionally narrow — enough to spawn / destroy a workspace
VM. Real per-machine env (volumes, `ROMMEL_WID` injection, image SHA pinning)
is set here; the pubkey is **not** injected per-machine because Phase 3
already baked it into the image (`workspace-image/Dockerfile`).

Dev mode: if `fly_api_token` is empty, every method short-circuits to a
deterministic stub. The lifecycle layer and the API surface don't have to
care which mode they're in.

Risk 4.7: the daemon URL template depends on the machine carrying a
`metadata.label = <wid>` so Fly's `.internal` DNS can resolve it. The plan
flags wiring this label here as the load-bearing detail.
"""

from __future__ import annotations

import uuid
from typing import TYPE_CHECKING

import httpx
import structlog

if TYPE_CHECKING:
    from api.config import Settings

logger = structlog.get_logger(__name__)

_FLY_API_BASE = "https://api.machines.dev/v1"


class FlyOrchestrator:
    def __init__(
        self,
        *,
        api_token: str,
        app_name: str,
        image: str,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        self._token = api_token
        self._app = app_name
        self._image = image
        self._client = client

    @classmethod
    def from_settings(cls, settings: "Settings") -> "FlyOrchestrator":
        return cls(
            api_token=settings.fly_api_token,
            app_name=settings.fly_workspaces_app,
            image=settings.workspace_image,
        )

    @property
    def _stubbed(self) -> bool:
        return not self._token

    async def _request(self, method: str, path: str, json: dict | None = None) -> httpx.Response:
        url = f"{_FLY_API_BASE}{path}"
        headers = {"Authorization": f"Bearer {self._token}"}
        if self._client is not None:
            resp = await self._client.request(method, url, json=json, headers=headers)
        else:
            async with httpx.AsyncClient(timeout=30.0) as client:
                resp = await client.request(method, url, json=json, headers=headers)
        resp.raise_for_status()
        return resp

    async def create_machine(self, *, wid: str, region: str | None) -> str:
        """Create a Fly Machine running the workspace image. Returns its id.

        The machine carries:
          - `metadata.label = <wid>` → Fly `.internal` DNS resolves
            `<wid>.vm.<app>.internal` to this machine (risk 4.7).
          - `env.ROMMEL_WID = <wid>` → the daemon's `config.FromEnv` requires this.
          - image = `settings.workspace_image` (Phase 3 baked the pubkey).
        """
        if self._stubbed:
            stub = f"stub-{uuid.uuid4().hex[:12]}"
            logger.info("fly.create_machine.stub", wid=wid, machine_id=stub)
            return stub

        body = {
            "name": wid,
            "region": region,
            "config": {
                "image": self._image,
                "env": {"ROMMEL_WID": wid},
                "metadata": {"label": wid},
                "auto_destroy": False,
                "restart": {"policy": "on-failure"},
            },
        }
        resp = await self._request("POST", f"/apps/{self._app}/machines", json=body)
        data = resp.json()
        return data["id"]

    async def start_machine(self, machine_id: str) -> None:
        if self._stubbed:
            logger.info("fly.start_machine.stub", machine_id=machine_id)
            return
        await self._request("POST", f"/apps/{self._app}/machines/{machine_id}/start")

    async def stop_machine(self, machine_id: str) -> None:
        if self._stubbed:
            logger.info("fly.stop_machine.stub", machine_id=machine_id)
            return
        await self._request("POST", f"/apps/{self._app}/machines/{machine_id}/stop")

    async def destroy_machine(self, machine_id: str) -> None:
        if self._stubbed:
            logger.info("fly.destroy_machine.stub", machine_id=machine_id)
            return
        await self._request("DELETE", f"/apps/{self._app}/machines/{machine_id}?force=true")
