// Hermetic tests for lib/pty.ts wrappers and the DaemonConnection event +
// notify paths that pty uses. Same FakeWebSocket pattern as fs-rpc and
// funnel-rpc, extended with serverPush() so a test can fake a server-pushed
// pty.output / pty.exit frame mid-flow.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { DaemonConnection } from "@/lib/daemon";
import { base64ToBytes, bytesToBase64, ptyClose, ptyInput, ptyOpen, ptyResize } from "@/lib/pty";

class FakeWebSocket {
  static OPEN = 1;
  static CLOSED = 3;
  readyState = 0;
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  onclose: ((ev: { code: number; reason: string }) => void) | null = null;
  sent: string[] = [];
  constructor(public url: string) {
    FakeWebSocket.instances.push(this);
    queueMicrotask(() => this.simulateOpen());
  }
  static instances: FakeWebSocket[] = [];
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({ code: 1000, reason: "" });
  }
  simulateOpen() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.({});
  }
  emitMessage(obj: unknown) {
    this.onmessage?.({ data: JSON.stringify(obj) });
  }
  // serverPush: emit an event-kind envelope (no id, kind="event"). Phase 7
  // additions like pty.output / pty.exit arrive this way.
  serverPush(type: string, payload: unknown) {
    this.emitMessage({ kind: "event", type, payload });
  }
}

async function makeReadyConn() {
  const conn = new DaemonConnection({
    url: "ws://localhost:7777/ws",
    token: "tok",
    webSocketImpl: FakeWebSocket as unknown as typeof WebSocket,
  });
  await conn.connect();
  return { conn, ws: FakeWebSocket.instances[FakeWebSocket.instances.length - 1]! };
}

beforeEach(() => {
  FakeWebSocket.instances = [];
});

describe("lib/pty — base64 helpers", () => {
  it("round-trips arbitrary bytes including null + high bits", () => {
    const bytes = new Uint8Array([0x00, 0x7f, 0x80, 0xff, 0x10, 0x9c]);
    const b64 = bytesToBase64(bytes);
    const back = base64ToBytes(b64);
    expect(Array.from(back)).toEqual(Array.from(bytes));
  });

  it("handles long buffers without blowing the call stack", () => {
    const big = new Uint8Array(100_000);
    for (let i = 0; i < big.length; i++) big[i] = i % 256;
    const b64 = bytesToBase64(big);
    const back = base64ToBytes(b64);
    expect(back.length).toBe(big.length);
    expect(back[0]).toBe(0);
    expect(back[255]).toBe(255);
  });
});

describe("lib/pty — RPC wrappers", () => {
  it("ptyOpen sends cols/rows and parses pty_id", async () => {
    const { conn, ws } = await makeReadyConn();
    const promise = ptyOpen(conn, { cols: 80, rows: 24 });
    const sent = JSON.parse(ws.sent[0]!) as { id: string; type: string; payload: { cols: number; rows: number } };
    expect(sent.type).toBe("pty.open");
    expect(sent.payload).toEqual({ cols: 80, rows: 24 });
    ws.emitMessage({
      kind: "response",
      type: "pty.open",
      id: sent.id,
      payload: { pty_id: "11111111-2222-3333-4444-555555555555" },
    });
    const res = await promise;
    expect(res.pty_id).toBe("11111111-2222-3333-4444-555555555555");
    conn.close();
  });

  it("ptyInput is fire-and-forget — sends but doesn't return a Promise", async () => {
    const { conn, ws } = await makeReadyConn();
    const result = ptyInput(conn, "pty-123", "hi\n");
    expect(result).toBeUndefined();
    expect(ws.sent).toHaveLength(1);
    const sent = JSON.parse(ws.sent[0]!) as {
      id: string;
      kind: string;
      payload: { pty_id: string; data: string };
    };
    expect(sent.kind).toBe("request");
    expect(sent.payload.pty_id).toBe("pty-123");
    expect(sent.payload.data).toBe(bytesToBase64(new TextEncoder().encode("hi\n")));
    // No matching response on success — the inflight slot self-cleans.
    conn.close();
  });

  it("ptyResize round-trips and resolves on empty {}", async () => {
    const { conn, ws } = await makeReadyConn();
    const promise = ptyResize(conn, "pty-1", 120, 40);
    const sent = JSON.parse(ws.sent[0]!) as {
      id: string;
      payload: { pty_id: string; cols: number; rows: number };
    };
    expect(sent.payload).toEqual({ pty_id: "pty-1", cols: 120, rows: 40 });
    ws.emitMessage({
      kind: "response",
      type: "pty.resize",
      id: sent.id,
      payload: {},
    });
    await expect(promise).resolves.toEqual({});
    conn.close();
  });

  it("ptyClose resolves; second call is idempotent on the daemon side", async () => {
    const { conn, ws } = await makeReadyConn();
    const p1 = ptyClose(conn, "pty-1");
    const sent1 = JSON.parse(ws.sent[0]!) as { id: string };
    ws.emitMessage({ kind: "response", type: "pty.close", id: sent1.id, payload: {} });
    await expect(p1).resolves.toEqual({});

    const p2 = ptyClose(conn, "pty-1");
    const sent2 = JSON.parse(ws.sent[1]!) as { id: string };
    // Daemon returns success again — that's the idempotency contract.
    ws.emitMessage({ kind: "response", type: "pty.close", id: sent2.id, payload: {} });
    await expect(p2).resolves.toEqual({});
    conn.close();
  });
});

describe("lib/pty — event subscription", () => {
  it("delivers pty.output events to subscribers filtered by pty_id", async () => {
    const { conn, ws } = await makeReadyConn();
    const handler = vi.fn();
    const unsub = conn.subscribe("pty.output", handler);

    const outBytes = new Uint8Array([0x68, 0x69, 0x0a]); // "hi\n"
    ws.serverPush("pty.output", { pty_id: "pty-1", data: bytesToBase64(outBytes) });

    expect(handler).toHaveBeenCalledOnce();
    const arg = handler.mock.calls[0]![0] as { pty_id: string; data: string };
    expect(arg.pty_id).toBe("pty-1");
    expect(Array.from(base64ToBytes(arg.data))).toEqual(Array.from(outBytes));

    unsub();
    ws.serverPush("pty.output", { pty_id: "pty-1", data: "ignored" });
    expect(handler).toHaveBeenCalledOnce();
    conn.close();
  });

  it("delivers pty.exit and pty.output_dropped events through subscribe", async () => {
    const { conn, ws } = await makeReadyConn();
    const onExit = vi.fn();
    const onDropped = vi.fn();
    conn.subscribe("pty.exit", onExit);
    conn.subscribe("pty.output_dropped", onDropped);

    ws.serverPush("pty.exit", { pty_id: "pty-1", exit_code: 0 });
    ws.serverPush("pty.output_dropped", { pty_id: "pty-1", dropped_count: 5 });

    expect(onExit).toHaveBeenCalledWith({ pty_id: "pty-1", exit_code: 0 });
    expect(onDropped).toHaveBeenCalledWith({ pty_id: "pty-1", dropped_count: 5 });
    conn.close();
  });
});

describe("lib/daemon — notify path (fire-and-forget for pty.input)", () => {
  it("sends a request frame but does not register a settled Promise", async () => {
    const { conn, ws } = await makeReadyConn();
    conn.notify("pty.input", { pty_id: "pty-1", data: bytesToBase64(new Uint8Array([0x61])) });
    expect(ws.sent).toHaveLength(1);
    const sent = JSON.parse(ws.sent[0]!) as { kind: string; type: string; id: string };
    expect(sent.kind).toBe("request");
    expect(sent.type).toBe("pty.input");
    expect(typeof sent.id).toBe("string");
    // No exception, no hung Promise — the slot self-cleans after the TTL.
    conn.close();
  });
});
