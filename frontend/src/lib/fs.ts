// Typed wrappers over conn.rpc() for the fs.* primitives. Thin by design:
// the wire contract is owned by @rommel/proto, the transport is owned by
// DaemonConnection — this file just bolts request/response types together so
// callers don't need to think about envelope plumbing.
//
// Phase 6 — fs.read / fs.list / fs.write.

import type {
  FsListRequest,
  FsListResponse,
  FsReadRequest,
  FsReadResponse,
  FsWriteRequest,
  FsWriteResponse,
} from "@rommel/proto";
import type { DaemonConnection } from "@/lib/daemon";

export async function fsList(conn: DaemonConnection, path: string): Promise<FsListResponse> {
  return conn.rpc<FsListRequest, FsListResponse>("fs.list", { path });
}

export async function fsRead(
  conn: DaemonConnection,
  path: string,
  encoding: FsReadRequest["encoding"] = "utf-8",
): Promise<FsReadResponse> {
  return conn.rpc<FsReadRequest, FsReadResponse>("fs.read", { path, encoding });
}

export async function fsWrite(
  conn: DaemonConnection,
  path: string,
  contents: string,
  encoding: FsWriteRequest["encoding"] = "utf-8",
): Promise<FsWriteResponse> {
  return conn.rpc<FsWriteRequest, FsWriteResponse>("fs.write", { path, contents, encoding });
}
