"use client";

import { useConnectionStore } from "@/stores/connection";
import { cn } from "@/lib/utils";

// Four states map to a Tailwind color (see tailwind.config.ts: pill.*).
const COLOR: Record<string, string> = {
  idle: "bg-zinc-500",
  connecting: "bg-pill-connecting",
  ready: "bg-pill-ready",
  reconnecting: "bg-pill-reconnecting",
  failed: "bg-pill-failed",
  closed: "bg-zinc-500",
};

const LABEL: Record<string, string> = {
  idle: "idle",
  connecting: "connecting",
  ready: "ready",
  reconnecting: "reconnecting",
  failed: "failed",
  closed: "closed",
};

export function ConnectionPill() {
  const { status, lastPong, sessionToken, lastError } = useConnectionStore();
  const dotClass = COLOR[status] ?? "bg-zinc-500";
  const label = LABEL[status] ?? status;
  const session = sessionToken ? sessionToken.slice(0, 6) + "…" : "—";

  return (
    <span
      data-testid="connection-pill"
      data-status={status}
      title={lastError ?? undefined}
      className="inline-flex items-center gap-2"
    >
      <span className={cn("inline-block h-2 w-2 rounded-full", dotClass)} aria-hidden />
      <span>{label}</span>
      <span className="text-zinc-600">·</span>
      <span>session {session}</span>
      {lastPong?.ok && <span className="text-emerald-400">ping ok</span>}
    </span>
  );
}
