// Placeholder for the .rommel/ planning funnel UI. Phase 6 / 7 territory —
// FunnelBoard reads .rommel/{triage,plans,next-up,executing,completions,archive}/*
// via the daemon's fs primitives and renders a kanban-style board.
// Listed here so the directory layout matches the plan.

export function FunnelBoard() {
  return (
    <div className="p-6 text-sm text-zinc-500">
      Funnel UI lives here in Phase 6+. See <code>docs/vision.md</code> §Layer 2.
    </div>
  );
}
