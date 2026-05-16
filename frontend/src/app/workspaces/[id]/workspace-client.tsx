"use client";

import { useState } from "react";
import { Header } from "@/components/shell/Header";
import { StatusBar } from "@/components/shell/StatusBar";
import { ConnectionPill } from "@/components/shell/ConnectionPill";
import { GitStatusPill } from "@/components/shell/GitStatusPill";
import { FileTree } from "@/components/filetree/FileTree";
import { EditorPane } from "@/components/editor/EditorPane";
import { TerminalTabs } from "@/components/terminal/TerminalTabs";
import { FunnelBoard } from "@/components/funnel/FunnelBoard";
import { useDaemonConnection } from "@/hooks/useDaemonConnection";
import { cn } from "@/lib/utils";
import type { Workspace } from "@/types/workspace";

type View = "ide" | "funnel";

// Two-pane editor/terminal layout, plus a Funnel kanban toggle in the header.
// Phase 6: the file tree is real, the editor opens/saves real files, the
// funnel board reads rommel/<stage>/.

export function WorkspaceClient({ workspace }: { workspace: Workspace }) {
  useDaemonConnection(workspace.id);
  const [view, setView] = useState<View>("ide");

  return (
    <div className="flex h-screen flex-col">
      <Header workspace={workspace}>
        <ViewToggle view={view} onChange={setView} />
      </Header>
      <div className="grid min-h-0 flex-1 grid-cols-[16rem_1fr] grid-rows-1 border-t border-zinc-800">
        <aside className="min-h-0 overflow-y-auto border-r border-zinc-800 bg-zinc-900/40">
          <FileTree />
        </aside>
        <main className="min-h-0">
          {view === "ide" ? (
            <div className="grid h-full min-h-0 grid-rows-[1fr_14rem]">
              <section className="monaco-host min-h-0 overflow-hidden">
                <EditorPane />
              </section>
              <section className="xterm-host min-h-0 overflow-hidden border-t border-zinc-800">
                <TerminalTabs />
              </section>
            </div>
          ) : (
            <section data-testid="funnel-view" className="h-full min-h-0 overflow-hidden">
              <FunnelBoard />
            </section>
          )}
        </main>
      </div>
      <StatusBar>
        <GitStatusPill />
        <ConnectionPill />
      </StatusBar>
    </div>
  );
}

function ViewToggle({ view, onChange }: { view: View; onChange: (v: View) => void }) {
  return (
    <div
      className="inline-flex overflow-hidden rounded border border-zinc-700 text-xs"
      role="tablist"
      aria-label="Workspace view"
    >
      {(["ide", "funnel"] as const).map((v) => (
        <button
          key={v}
          type="button"
          role="tab"
          aria-selected={view === v}
          data-testid={`view-toggle-${v}`}
          onClick={() => onChange(v)}
          className={cn(
            "px-3 py-1 capitalize",
            view === v ? "bg-zinc-800 text-zinc-100" : "text-zinc-400 hover:bg-zinc-800/40",
          )}
        >
          {v === "ide" ? "IDE" : "Funnel"}
        </button>
      ))}
    </div>
  );
}
