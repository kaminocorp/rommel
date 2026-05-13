"use client";

import { Header } from "@/components/shell/Header";
import { StatusBar } from "@/components/shell/StatusBar";
import { ConnectionPill } from "@/components/shell/ConnectionPill";
import { FileTree } from "@/components/filetree/FileTree";
import { EditorPane } from "@/components/editor/EditorPane";
import { TerminalPane } from "@/components/terminal/TerminalPane";
import { useDaemonConnection } from "@/hooks/useDaemonConnection";
import type { Workspace } from "@/types/workspace";

// Two-pane editor/terminal layout. Plan §step-5.
//
//  ┌────────────────┬──────────────────────────┐
//  │  FileTree      │   EditorPane (Monaco)    │
//  │  (stub)        │                          │
//  │                ├──────────────────────────┤
//  │                │   TerminalPane (xterm)   │
//  └────────────────┴──────────────────────────┘

export function WorkspaceClient({ workspace }: { workspace: Workspace }) {
  useDaemonConnection(workspace.id);

  return (
    <div className="flex h-screen flex-col">
      <Header workspace={workspace} />
      <div className="grid min-h-0 flex-1 grid-cols-[16rem_1fr] grid-rows-1 border-t border-zinc-800">
        <aside className="min-h-0 overflow-y-auto border-r border-zinc-800 bg-zinc-900/40">
          <FileTree />
        </aside>
        <main className="grid min-h-0 grid-rows-[1fr_14rem]">
          <section className="monaco-host min-h-0 overflow-hidden">
            <EditorPane />
          </section>
          <section className="xterm-host min-h-0 overflow-hidden border-t border-zinc-800">
            <TerminalPane />
          </section>
        </main>
      </div>
      <StatusBar>
        <ConnectionPill />
      </StatusBar>
    </div>
  );
}
