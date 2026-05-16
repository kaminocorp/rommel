// Typed wrappers over conn.rpc() / conn.notify() for the pty.* primitives.
//
// Phase 7 — pty.open / pty.input / pty.resize / pty.close, plus three
// server-pushed events: pty.output, pty.exit, pty.output_dropped.
//
// pty.input is fire-and-forget (success is silent on the wire); the wrapper
// hides this by exposing a sync `void` API and letting DaemonConnection
// surface errors through its console-warn fallback. resize and close use
// the regular request/response path (the daemon returns `{}` on success).
//
// Byte ↔ base64 conversion stays browser-native (btoa / atob) so the bundle
// doesn't pull in `Buffer`. PTY output is arbitrary bytes — escape sequences,
// partial UTF-8 — so we never round-trip through `string`.

import type {
  PtyCloseRequest,
  PtyCloseResponse,
  PtyInput,
  PtyOpenRequest,
  PtyOpenResponse,
  PtyResizeRequest,
  PtyResizeResponse,
  PtyStartAgentRequest,
  PtyStartAgentResponse,
} from "@rommel/proto";
import type { DaemonConnection } from "@/lib/daemon";

export async function ptyOpen(
  conn: DaemonConnection,
  req: PtyOpenRequest,
): Promise<PtyOpenResponse> {
  return conn.rpc<PtyOpenRequest, PtyOpenResponse>("pty.open", req);
}

export function ptyInput(
  conn: DaemonConnection,
  ptyId: string,
  data: Uint8Array | string,
): void {
  const bytes = typeof data === "string" ? new TextEncoder().encode(data) : data;
  conn.notify<PtyInput>("pty.input", {
    pty_id: ptyId,
    data: bytesToBase64(bytes),
  });
}

export async function ptyResize(
  conn: DaemonConnection,
  ptyId: string,
  cols: number,
  rows: number,
): Promise<PtyResizeResponse> {
  return conn.rpc<PtyResizeRequest, PtyResizeResponse>("pty.resize", {
    pty_id: ptyId,
    cols,
    rows,
  });
}

export async function ptyClose(
  conn: DaemonConnection,
  ptyId: string,
): Promise<PtyCloseResponse> {
  return conn.rpc<PtyCloseRequest, PtyCloseResponse>("pty.close", {
    pty_id: ptyId,
  });
}

export async function ptyStartAgent(
  conn: DaemonConnection,
  req: PtyStartAgentRequest,
): Promise<PtyStartAgentResponse> {
  return conn.rpc<PtyStartAgentRequest, PtyStartAgentResponse>("pty.start_agent", req);
}

// --- base64 helpers ---------------------------------------------------------

// btoa requires a binary string (one char per byte, 0–255). String.fromCharCode
// on a Uint8Array gives exactly that. For long buffers we chunk to avoid the
// "Maximum call stack size exceeded" trap from String.fromCharCode(...big).
export function bytesToBase64(bytes: Uint8Array): string {
  const CHUNK = 0x8000;
  let binary = "";
  for (let i = 0; i < bytes.length; i += CHUNK) {
    const slice = bytes.subarray(i, Math.min(i + CHUNK, bytes.length));
    binary += String.fromCharCode(...slice);
  }
  return btoa(binary);
}

export function base64ToBytes(b64: string): Uint8Array {
  const binary = atob(b64);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}
