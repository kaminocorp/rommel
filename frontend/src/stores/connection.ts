"use client";

import { create } from "zustand";
import type { ConnectionStatus } from "@/lib/daemon";

type LastPong = { ok: boolean; ts: string } | null;

type ConnectionState = {
  status: ConnectionStatus;
  sessionToken: string | null;
  daemonUrl: string | null;
  expiresAt: Date | null;
  lastError: string | null;
  lastPong: LastPong;
  setStatus(s: ConnectionStatus): void;
  setSession(args: { token: string; daemonUrl: string; expiresAt: Date }): void;
  setLastPong(p: LastPong): void;
  setLastError(msg: string | null): void;
  reset(): void;
};

const initial = {
  status: "idle" as ConnectionStatus,
  sessionToken: null,
  daemonUrl: null,
  expiresAt: null,
  lastError: null,
  lastPong: null as LastPong,
};

export const useConnectionStore = create<ConnectionState>((set) => ({
  ...initial,
  setStatus: (status) => set({ status }),
  setSession: ({ token, daemonUrl, expiresAt }) =>
    set({ sessionToken: token, daemonUrl, expiresAt }),
  setLastPong: (lastPong) => set({ lastPong }),
  setLastError: (lastError) => set({ lastError }),
  reset: () => set(initial),
}));
