// Hermetic tests for the lib/fs.ts typed wrappers. The wrappers call
// DaemonConnection.rpc under the hood; we drive the connection with a
// FakeWebSocket and assert the on-wire envelope shape matches the proto
// contract, then resolve the response and assert the typed result.

import { describe, it, expect, beforeEach } from "vitest";
import { DaemonConnection, DaemonProtocolError } from "@/lib/daemon";
import { fsList, fsRead, fsWrite } from "@/lib/fs";

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

describe("lib/fs typed wrappers", () => {
  it("fsList sends a request with the path and resolves the response", async () => {
    const { conn, ws } = await makeReadyConn();
    const p = fsList(conn, ".");
    const sent = JSON.parse(ws.sent[0]!) as { id: string; type: string; payload: { path: string } };
    expect(sent.type).toBe("fs.list");
    expect(sent.payload).toEqual({ path: "." });
    ws.emitMessage({
      kind: "response",
      type: "fs.list",
      id: sent.id,
      payload: {
        path: ".",
        entries: [
          { name: "a.txt", kind: "file", size: 1, mtime: "2026-05-13T00:00:00Z" },
          { name: "sub", kind: "dir", size: 0, mtime: "2026-05-13T00:00:00Z" },
        ],
      },
    });
    const res = await p;
    expect(res.entries).toHaveLength(2);
    expect(res.entries[0]?.kind).toBe("file");
    expect(res.entries[1]?.kind).toBe("dir");
    conn.close();
  });

  it("fsRead defaults encoding to utf-8", async () => {
    const { conn, ws } = await makeReadyConn();
    const p = fsRead(conn, "README.md");
    const sent = JSON.parse(ws.sent[0]!) as {
      id: string;
      type: string;
      payload: { path: string; encoding: string };
    };
    expect(sent.type).toBe("fs.read");
    expect(sent.payload.encoding).toBe("utf-8");
    ws.emitMessage({
      kind: "response",
      type: "fs.read",
      id: sent.id,
      payload: {
        path: "README.md",
        contents: "# hi",
        encoding: "utf-8",
        size: 4,
        mtime: "2026-05-13T00:00:00Z",
      },
    });
    const res = await p;
    expect(res.contents).toBe("# hi");
    conn.close();
  });

  it("fsWrite sends path + contents and surfaces errors", async () => {
    const { conn, ws } = await makeReadyConn();
    const p = fsWrite(conn, "out.txt", "hello");
    const sent = JSON.parse(ws.sent[0]!) as {
      id: string;
      payload: { path: string; contents: string; encoding: string };
    };
    expect(sent.payload).toEqual({ path: "out.txt", contents: "hello", encoding: "utf-8" });
    ws.emitMessage({
      kind: "error",
      type: "fs.write",
      id: sent.id,
      error: { code: "fs.invalid_path", message: "nope" },
    });
    await expect(p).rejects.toBeInstanceOf(DaemonProtocolError);
    conn.close();
  });

  it("fsWrite passes through encoding=base64", async () => {
    const { conn, ws } = await makeReadyConn();
    // close() rejects the in-flight rpc — attach a catch so the rejection is
    // observed and Node doesn't flag an unhandled rejection in the test runner.
    const p = fsWrite(conn, "b.bin", "AP8Q", "base64").catch(() => undefined);
    const sent = JSON.parse(ws.sent[0]!) as { payload: { encoding: string } };
    expect(sent.payload.encoding).toBe("base64");
    conn.close();
    await p;
  });
});
