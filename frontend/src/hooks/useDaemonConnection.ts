"use client";

import { useEffect } from "react";
import { DaemonConnection } from "@/lib/daemon";
import { useConnectionStore } from "@/stores/connection";
import { useCreateSession } from "./useWorkspace";

// Mount this hook in the workspace page; it manages the full lifecycle:
// POST /sessions → open WS → send system.ping → store result.
// Cleanup tears the socket down on unmount.
//
// Phase 6: the resolved DaemonConnection is now stashed in the connection
// store so sibling components (FileTree, FunnelBoard, EditorPane) can RPC
// against the same socket. Set once after connect() resolves; cleared on
// unmount.

export function useDaemonConnection(workspaceId: string): void {
  const { mutateAsync: createSession } = useCreateSession(workspaceId);
  const store = useConnectionStore();

  useEffect(() => {
    let conn: DaemonConnection | null = null;
    let cancelled = false;

    const refresh = async () => {
      const next = await createSession();
      return {
        url: next.daemon_url,
        token: next.token,
        expiresAt: new Date(next.expires_at),
      };
    };

    void (async () => {
      try {
        store.setLastError(null);
        store.setStatus("connecting");
        const session = await createSession();
        if (cancelled) return;
        const expiresAt = new Date(session.expires_at);
        store.setSession({ token: session.token, daemonUrl: session.daemon_url, expiresAt });

        conn = new DaemonConnection({
          url: session.daemon_url,
          token: session.token,
          expiresAt,
          onStatusChange: store.setStatus,
          refresh,
        });
        await conn.connect();
        if (cancelled) {
          conn.close();
          return;
        }
        store.setDaemon(conn);

        const pong = await conn.rpc<Record<string, never>, { ok: boolean; ts: string }>(
          "system.ping",
          {},
        );
        if (!cancelled) store.setLastPong(pong);
      } catch (e) {
        if (cancelled) return;
        store.setLastError(e instanceof Error ? e.message : String(e));
        store.setStatus("failed");
      }
    })();

    return () => {
      cancelled = true;
      conn?.close();
      store.setDaemon(null);
      // Don't reset the rest of the store on unmount — the workspace page may
      // re-mount under StrictMode and we want the pill to keep its state
      // visible.
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspaceId]);
}
