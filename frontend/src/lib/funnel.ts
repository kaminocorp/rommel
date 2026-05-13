// Typed wrappers over conn.rpc() for the funnel.* primitives.
//
// The valid-transition table from Phase 6 plan §0.5 is mirrored here as
// `validNextStages()` — used by the FE to filter promote-dropdown options
// before they reach the daemon. The daemon enforces the same table
// server-side; the FE filter is a UX nicety, not a security boundary.

import type {
  FunnelListRequest,
  FunnelListResponse,
  FunnelPromoteRequest,
  FunnelPromoteResponse,
  FunnelReadRequest,
  FunnelReadResponse,
  FunnelStage,
} from "@rommel/proto";
import type { DaemonConnection } from "@/lib/daemon";

export const FUNNEL_STAGES: readonly FunnelStage[] = [
  "triage",
  "plans",
  "next-up",
  "executing",
  "completions",
  "archive",
] as const;

export const FUNNEL_STAGE_LABEL: Record<FunnelStage, string> = {
  triage: "Triage",
  plans: "Plans",
  "next-up": "Next Up",
  executing: "Executing",
  completions: "Completions",
  archive: "Archive",
};

// Mirrors sandbox-daemon/internal/funnel/handler.go::isValidTransition.
// Forward-only along the chain, plus archive-from-anywhere as a kill switch.
export function validNextStages(from: FunnelStage): FunnelStage[] {
  const idx = FUNNEL_STAGES.indexOf(from);
  const out: FunnelStage[] = [];
  const next = FUNNEL_STAGES[idx + 1];
  if (next) out.push(next);
  if (from !== "archive" && !out.includes("archive")) out.push("archive");
  return out;
}

export async function funnelList(
  conn: DaemonConnection,
  stage: FunnelStage,
): Promise<FunnelListResponse> {
  return conn.rpc<FunnelListRequest, FunnelListResponse>("funnel.list", { stage });
}

export async function funnelRead(
  conn: DaemonConnection,
  stage: FunnelStage,
  name: string,
): Promise<FunnelReadResponse> {
  return conn.rpc<FunnelReadRequest, FunnelReadResponse>("funnel.read", {
    stage: stage as FunnelReadRequest["stage"],
    name,
  });
}

export async function funnelPromote(
  conn: DaemonConnection,
  name: string,
  from: FunnelStage,
  to: FunnelStage,
): Promise<FunnelPromoteResponse> {
  return conn.rpc<FunnelPromoteRequest, FunnelPromoteResponse>("funnel.promote", {
    name,
    from: from as FunnelPromoteRequest["from"],
    to: to as FunnelPromoteRequest["to"],
  });
}
