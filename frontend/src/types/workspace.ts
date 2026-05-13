// Mirrors backend/api/workspaces.py::WorkspaceOut and sessions.py::SessionOut.
// Hand-rolled in v1 — OpenAPI-derived types come once the backend exports
// /openapi.json reliably.

export type WorkspaceStatus = "creating" | "running" | "stopped" | "error";

export type Workspace = {
  id: string;
  name: string;
  status: WorkspaceStatus | string;
  fly_machine_id: string | null;
  created_at: string;
  updated_at: string;
};

export type SessionResponse = {
  daemon_url: string;
  token: string;
  // RFC3339 / ISO 8601 string from FastAPI's datetime serializer.
  expires_at: string;
};

export type UserClaims = {
  sub: string;
  email?: string;
};
