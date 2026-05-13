// Hermetic unit tests for DaemonConnection. Plan §step-4.
// Strategy: a tiny fake WebSocket that records `send()` calls and exposes
// `emit*` helpers so tests can simulate the daemon's responses synchronously.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { DaemonConnection, DaemonProtocolError } from "@/lib/daemon";

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
  close(_code?: number, _reason?: string) {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({ code: _code ?? 1000, reason: _reason ?? "" });
  }
  simulateOpen() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.({});
  }
  emitMessage(obj: unknown) {
    this.onmessage?.({ data: JSON.stringify(obj) });
  }
  emitClose(code: number) {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({ code, reason: "" });
  }
}

function makeConn(extra: Partial<ConstructorParameters<typeof DaemonConnection>[0]> = {}) {
  return new DaemonConnection({
    url: "ws://localhost:7777/ws",
    token: "tok-abc",
    webSocketImpl: FakeWebSocket as unknown as typeof WebSocket,
    ...extra,
  });
}

beforeEach(() => {
  FakeWebSocket.instances = [];
});

describe("DaemonConnection", () => {
  it("appends ?token=... if missing on the URL", async () => {
    const conn = makeConn();
    await conn.connect();
    const ws = FakeWebSocket.instances[0];
    expect(ws).toBeDefined();
    expect(ws!.url).toBe("ws://localhost:7777/ws?token=tok-abc");
    conn.close();
  });

  it("does not duplicate the token if URL already has one", async () => {
    const conn = makeConn({ url: "ws://localhost:7777/ws?token=existing" });
    await conn.connect();
    const ws = FakeWebSocket.instances[0];
    expect(ws!.url).toBe("ws://localhost:7777/ws?token=existing");
    conn.close();
  });

  it("correlates rpc() request and response by id", async () => {
    const conn = makeConn();
    await conn.connect();
    const ws = FakeWebSocket.instances[0]!;
    const promise = conn.rpc<Record<string, never>, { ok: boolean }>("system.ping", {});
    // The wrapper sent one frame; pluck its id and echo a response.
    expect(ws.sent).toHaveLength(1);
    const sent = JSON.parse(ws.sent[0]!) as { id: string; type: string; kind: string };
    expect(sent.kind).toBe("request");
    expect(sent.type).toBe("system.ping");
    expect(sent.id).toBeTruthy();

    ws.emitMessage({
      kind: "response",
      type: "system.ping",
      id: sent.id,
      payload: { ok: true },
    });

    await expect(promise).resolves.toEqual({ ok: true });
    conn.close();
  });

  it("rejects rpc() when daemon returns an error envelope", async () => {
    const conn = makeConn();
    await conn.connect();
    const ws = FakeWebSocket.instances[0]!;
    const promise = conn.rpc("fs.read", { path: "no/such" });
    const sent = JSON.parse(ws.sent[0]!) as { id: string };
    ws.emitMessage({
      kind: "error",
      type: "fs.read",
      id: sent.id,
      error: { code: "fs.not_found", message: "not found" },
    });
    await expect(promise).rejects.toBeInstanceOf(DaemonProtocolError);
    conn.close();
  });

  it("fans out events to subscribers", async () => {
    const conn = makeConn();
    await conn.connect();
    const ws = FakeWebSocket.instances[0]!;
    const handler = vi.fn();
    const unsub = conn.subscribe("pty.output", handler);
    ws.emitMessage({ kind: "event", type: "pty.output", payload: { data: "hi" } });
    expect(handler).toHaveBeenCalledOnce();
    expect(handler).toHaveBeenCalledWith({ data: "hi" });
    unsub();
    ws.emitMessage({ kind: "event", type: "pty.output", payload: { data: "after" } });
    expect(handler).toHaveBeenCalledOnce();
    conn.close();
  });

  it("transitions through connecting → ready → closed", async () => {
    const transitions: string[] = [];
    const conn = makeConn({ onStatusChange: (s) => transitions.push(s) });
    await conn.connect();
    conn.close();
    expect(transitions).toContain("connecting");
    expect(transitions).toContain("ready");
    expect(transitions).toContain("closed");
  });

  it("rejects in-flight rpc() on close (risk 4.9)", async () => {
    const conn = makeConn();
    await conn.connect();
    const promise = conn.rpc("fs.read", {});
    conn.close();
    await expect(promise).rejects.toMatchObject({ name: "DaemonClosedError" });
  });

  it("invokes refresh() on close-1008 (token expired)", async () => {
    const refresh = vi.fn(async () => ({
      url: "ws://localhost:7777/ws",
      token: "tok-fresh",
    }));
    const conn = makeConn({ refresh });
    await conn.connect();
    const ws = FakeWebSocket.instances[0]!;
    ws.emitClose(1008);
    // refresh runs in a microtask
    await new Promise((r) => setTimeout(r, 0));
    expect(refresh).toHaveBeenCalled();
    conn.close();
  });
});
