"use client";

import { create } from "zustand";
import type { ConnectionStatus, DaemonConnection } from "@/lib/daemon";

type LastPong = { ok: boolean; ts: string } | null;

type ConnectionState = {
  status: ConnectionStatus;
  sessionToken: string | null;
  daemonUrl: string | null;
  expiresAt: Date | null;
  lastError: string | null;
  lastPong: LastPong;
  // Phase 6: shared DaemonConnection ref so FileTree / FunnelBoard / Editor
  // all RPC through the same socket. Set once by useDaemonConnection after
  // connect() resolves; cleared on unmount via reset().
  daemon: DaemonConnection | null;
  // Phase 6: workspace-scoped UI state — which file the EditorPane should
  // load + the dirty bit. Kept here rather than locally in EditorPane so the
  // FileTree click handler can poke it without prop-drilling.
  selectedFile: string | null;
  setStatus(s: ConnectionStatus): void;
  setSession(args: { token: string; daemonUrl: string; expiresAt: Date }): void;
  setLastPong(p: LastPong): void;
  setLastError(msg: string | null): void;
  setDaemon(d: DaemonConnection | null): void;
  selectFile(path: string | null): void;
  reset(): void;
};

const initial = {
  status: "idle" as ConnectionStatus,
  sessionToken: null,
  daemonUrl: null,
  expiresAt: null,
  lastError: null,
  lastPong: null as LastPong,
  daemon: null as DaemonConnection | null,
  selectedFile: null as string | null,
};

export const useConnectionStore = create<ConnectionState>((set) => ({
  ...initial,
  setStatus: (status) => set({ status }),
  setSession: ({ token, daemonUrl, expiresAt }) =>
    set({ sessionToken: token, daemonUrl, expiresAt }),
  setLastPong: (lastPong) => set({ lastPong }),
  setLastError: (lastError) => set({ lastError }),
  setDaemon: (daemon) => set({ daemon }),
  selectFile: (selectedFile) => set({ selectedFile }),
  reset: () => set(initial),
}));
