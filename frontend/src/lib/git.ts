// Typed wrappers for git.* primitives.

import type { GitStatusRequest, GitStatusResponse } from "@rommel/proto";
import type { DaemonConnection } from "@/lib/daemon";

export async function gitStatus(
  conn: DaemonConnection,
  path?: string,
): Promise<GitStatusResponse> {
  const payload: GitStatusRequest = path ? { path } : {};
  return conn.rpc<GitStatusRequest, GitStatusResponse>("git.status", payload);
}

export async function gitDiff(conn: DaemonConnection, path?: string, staged = false) {
  return conn.rpc("git.diff", { path, staged });
}

export async function gitBranchList(conn: DaemonConnection) {
  return conn.rpc("git.branch.list", {});
}

export async function gitBranchCreate(conn: DaemonConnection, name: string, checkout = true) {
  return conn.rpc("git.branch.create", { name, checkout });
}

export async function gitBranchSwitch(conn: DaemonConnection, name: string) {
  return conn.rpc("git.branch.switch", { name });
}

export async function gitCommit(conn: DaemonConnection, message: string, files?: string[]) {
  return conn.rpc("git.commit", { message, files });
}
