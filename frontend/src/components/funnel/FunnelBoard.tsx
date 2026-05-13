"use client";

// funnel.* — kanban over rommel/<stage>/. Phase 6.
//
// Six columns, one per stage. Each entry is a card with name + a Promote ▸
// dropdown listing valid next stages. Clicking a promote target fires
// funnel.promote; on success TanStack invalidates both source and destination
// lists so the board re-renders.
//
// Defer: card body preview (would be 6 × N fs/funnel.read calls on mount);
// drag-to-promote (a v2 affordance); card edit (use fs.write under the hood
// once the editor surface accepts arbitrary paths).

import { useState } from "react";
import { ChevronDown } from "lucide-react";
import type { FunnelStage } from "@rommel/proto";
import { useFunnelList, useFunnelPromote } from "@/hooks/useFunnel";
import { FUNNEL_STAGES, FUNNEL_STAGE_LABEL, validNextStages } from "@/lib/funnel";
import { cn } from "@/lib/utils";
import { useConnectionStore } from "@/stores/connection";

export function FunnelBoard() {
  return (
    <div className="grid h-full min-h-0 grid-cols-6 gap-2 overflow-x-auto bg-zinc-950 p-3">
      {FUNNEL_STAGES.map((stage) => (
        <StageColumn key={stage} stage={stage} />
      ))}
    </div>
  );
}

function StageColumn({ stage }: { stage: FunnelStage }) {
  const { data, isLoading, isError, error } = useFunnelList(stage);
  return (
    <section
      data-testid="funnel-column"
      data-stage={stage}
      className="flex min-h-0 flex-col rounded border border-zinc-800 bg-zinc-900/40"
    >
      <header className="border-b border-zinc-800 px-3 py-2 text-xs font-medium uppercase tracking-wide text-zinc-400">
        {FUNNEL_STAGE_LABEL[stage]}
        <span className="ml-2 text-zinc-600">{data?.entries.length ?? "·"}</span>
      </header>
      <div className="flex-1 space-y-2 overflow-y-auto p-2 text-xs">
        {isLoading && <p className="text-zinc-600">loading…</p>}
        {isError && (
          <p className="text-rose-500">error: {(error as Error)?.message ?? "unknown"}</p>
        )}
        {data?.entries.map((entry) => (
          <Card key={entry.name} stage={stage} name={entry.name} />
        ))}
        {data && data.entries.length === 0 && <p className="text-zinc-600">empty</p>}
      </div>
    </section>
  );
}

function Card({ stage, name }: { stage: FunnelStage; name: string }) {
  const [menuOpen, setMenuOpen] = useState(false);
  const { mutate: promote, isPending } = useFunnelPromote();
  const selectFile = useConnectionStore((s) => s.selectFile);
  const nextStages = validNextStages(stage);

  // Open the card in the editor by selecting its path. rommel/<stage>/<name>
  // is workspace-relative — fs.read sandboxes it.
  function openInEditor() {
    selectFile(`rommel/${stage}/${name}`);
  }

  return (
    <div
      data-testid="funnel-card"
      data-name={name}
      className="rounded border border-zinc-800 bg-zinc-900 p-2 hover:border-zinc-700"
    >
      <button
        type="button"
        onClick={openInEditor}
        className="block w-full truncate text-left font-mono text-zinc-200"
        title={name}
      >
        {name}
      </button>
      <div className="mt-1 flex items-center justify-end">
        <div className="relative">
          <button
            type="button"
            onClick={() => setMenuOpen((o) => !o)}
            disabled={isPending || nextStages.length === 0}
            data-testid="funnel-promote-button"
            className={cn(
              "inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-xs",
              "border border-zinc-700 text-zinc-400 hover:bg-zinc-800",
              "disabled:opacity-50",
            )}
          >
            Promote
            <ChevronDown className="size-3" />
          </button>
          {menuOpen && nextStages.length > 0 && (
            <div className="absolute right-0 z-10 mt-1 w-32 rounded border border-zinc-700 bg-zinc-900 py-1 text-xs shadow-lg">
              {nextStages.map((to) => (
                <button
                  key={to}
                  type="button"
                  onClick={() => {
                    setMenuOpen(false);
                    promote({ name, from: stage, to });
                  }}
                  data-testid="funnel-promote-target"
                  data-to={to}
                  className="block w-full px-3 py-1 text-left text-zinc-200 hover:bg-zinc-800"
                >
                  → {FUNNEL_STAGE_LABEL[to]}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
