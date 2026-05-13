// Hermetic tests for lib/funnel.ts — same FakeWebSocket pattern as fs-rpc.

import { describe, it, expect, beforeEach } from "vitest";
import { DaemonConnection } from "@/lib/daemon";
import {
  FUNNEL_STAGES,
  funnelList,
  funnelPromote,
  funnelRead,
  validNextStages,
} from "@/lib/funnel";

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

describe("lib/funnel — pure helpers", () => {
  it("FUNNEL_STAGES is the canonical six-stage ordering", () => {
    expect(FUNNEL_STAGES).toEqual([
      "triage",
      "plans",
      "next-up",
      "executing",
      "completions",
      "archive",
    ]);
  });

  it("validNextStages mirrors the daemon transition table", () => {
    expect(validNextStages("triage")).toEqual(["plans", "archive"]);
    expect(validNextStages("plans")).toEqual(["next-up", "archive"]);
    expect(validNextStages("next-up")).toEqual(["executing", "archive"]);
    expect(validNextStages("executing")).toEqual(["completions", "archive"]);
    expect(validNextStages("completions")).toEqual(["archive"]);
    expect(validNextStages("archive")).toEqual([]);
  });
});

describe("lib/funnel — RPC wrappers", () => {
  it("funnelList sends stage and unwraps entries", async () => {
    const { conn, ws } = await makeReadyConn();
    const p = funnelList(conn, "triage");
    const sent = JSON.parse(ws.sent[0]!) as { id: string; type: string; payload: { stage: string } };
    expect(sent.type).toBe("funnel.list");
    expect(sent.payload).toEqual({ stage: "triage" });
    ws.emitMessage({
      kind: "response",
      type: "funnel.list",
      id: sent.id,
      payload: {
        stage: "triage",
        entries: [{ name: "card.md", size: 5, mtime: "2026-05-13T00:00:00Z" }],
      },
    });
    const res = await p;
    expect(res.entries[0]?.name).toBe("card.md");
    conn.close();
  });

  it("funnelRead sends stage+name and unwraps contents", async () => {
    const { conn, ws } = await makeReadyConn();
    const p = funnelRead(conn, "plans", "plan-x.md");
    const sent = JSON.parse(ws.sent[0]!) as {
      id: string;
      payload: { stage: string; name: string };
    };
    expect(sent.payload).toEqual({ stage: "plans", name: "plan-x.md" });
    ws.emitMessage({
      kind: "response",
      type: "funnel.read",
      id: sent.id,
      payload: {
        stage: "plans",
        name: "plan-x.md",
        contents: "# x",
        size: 3,
        mtime: "2026-05-13T00:00:00Z",
      },
    });
    const res = await p;
    expect(res.contents).toBe("# x");
    conn.close();
  });

  it("funnelPromote sends name, from, to", async () => {
    const { conn, ws } = await makeReadyConn();
    const p = funnelPromote(conn, "card.md", "triage", "plans");
    const sent = JSON.parse(ws.sent[0]!) as {
      id: string;
      payload: { name: string; from: string; to: string };
    };
    expect(sent.payload).toEqual({ name: "card.md", from: "triage", to: "plans" });
    ws.emitMessage({
      kind: "response",
      type: "funnel.promote",
      id: sent.id,
      payload: {
        name: "card.md",
        from: "triage",
        to: "plans",
        mtime: "2026-05-13T00:00:00Z",
      },
    });
    const res = await p;
    expect(res.to).toBe("plans");
    conn.close();
  });
});
