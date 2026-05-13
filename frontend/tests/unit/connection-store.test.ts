import { describe, it, expect, beforeEach } from "vitest";
import { useConnectionStore } from "@/stores/connection";

describe("connection store", () => {
  beforeEach(() => {
    useConnectionStore.getState().reset();
  });

  it("starts in idle status", () => {
    expect(useConnectionStore.getState().status).toBe("idle");
  });

  it("setStatus updates the status field only", () => {
    useConnectionStore.getState().setStatus("ready");
    expect(useConnectionStore.getState().status).toBe("ready");
    expect(useConnectionStore.getState().sessionToken).toBeNull();
  });

  it("setSession captures token, daemonUrl, expiresAt", () => {
    const exp = new Date(Date.now() + 5 * 60_000);
    useConnectionStore
      .getState()
      .setSession({ token: "tok-abc", daemonUrl: "ws://x/ws", expiresAt: exp });
    const s = useConnectionStore.getState();
    expect(s.sessionToken).toBe("tok-abc");
    expect(s.daemonUrl).toBe("ws://x/ws");
    expect(s.expiresAt).toBe(exp);
  });

  it("setLastPong records the latest pong payload", () => {
    useConnectionStore.getState().setLastPong({ ok: true, ts: "2026-05-13T00:00:00Z" });
    expect(useConnectionStore.getState().lastPong?.ok).toBe(true);
  });

  it("reset returns to initial state", () => {
    useConnectionStore.getState().setStatus("ready");
    useConnectionStore.getState().setLastError("boom");
    useConnectionStore.getState().reset();
    const s = useConnectionStore.getState();
    expect(s.status).toBe("idle");
    expect(s.lastError).toBeNull();
  });
});
