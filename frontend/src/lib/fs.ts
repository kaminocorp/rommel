// Typed wrappers over conn.rpc() for the fs.* primitives. Thin by design:
// the wire contract is owned by @rommel/proto, the transport is owned by
// DaemonConnection — this file just bolts request/response types together so
// callers don't need to think about envelope plumbing.
//
// Phase 6 — fs.read / fs.list / fs.write.

import type {
  FsDeleteRequest,
  FsDeleteResponse,
  FsListRequest,
  FsListResponse,
  FsMkdirRequest,
  FsMkdirResponse,
  FsMoveRequest,
  FsMoveResponse,
  FsReadRequest,
  FsReadResponse,
  FsWatchRequest,
  FsWatchResponse,
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

// fsWatch starts a server-pushed watch on the given path. The caller must
// subsequently call conn.subscribe("fs.watch-event", handler) to receive
// FsWatchEvent payloads. The watch is automatically cleaned up when the
// connection drops (daemon-side OnDisconnect).
export async function fsWatch(
  conn: DaemonConnection,
  path: string,
  recursive: FsWatchRequest["recursive"] = false,
): Promise<FsWatchResponse> {
  return conn.rpc<FsWatchRequest, FsWatchResponse>("fs.watch", { path, recursive });
}

export async function fsMkdir(
  conn: DaemonConnection,
  path: string,
  recursive = false,
): Promise<FsMkdirResponse> {
  return conn.rpc<FsMkdirRequest, FsMkdirResponse>("fs.mkdir", { path, recursive });
}

export async function fsMove(
  conn: DaemonConnection,
  fromPath: string,
  toPath: string,
): Promise<FsMoveResponse> {
  return conn.rpc<FsMoveRequest, FsMoveResponse>("fs.move", { from: fromPath, to: toPath });
}

export async function fsDelete(
  conn: DaemonConnection,
  path: string,
  recursive = false,
): Promise<FsDeleteResponse> {
  return conn.rpc<FsDeleteRequest, FsDeleteResponse>("fs.delete", { path, recursive });
}
