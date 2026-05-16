"use client";

import { useState } from "react";
import dynamic from "next/dynamic";
import { cn } from "@/lib/utils";

const XtermImpl = dynamic(
  () => import("./xterm-impl").then((m) => m.XtermImpl),
  {
    ssr: false,
    loading: () => (
      <div className="flex h-full items-center justify-center text-xs text-zinc-500">
        Loading terminal…
      </div>
    ),
  }
);

type Tab = {
  key: number;
  label: string;
};

const MAX_TABS = 4;

/**
 * TerminalTabs (Phase 3.1) — supports up to MAX_TABS concurrent PTYs.
 * Each mounted XtermImpl opens its own PTY via usePty (daemon already
 * enforces the soft cap of 4 per connection).
 *
 * - "+" button opens a fresh shell PTY in a new tab.
 * - Clicking a tab switches the visible pane (other PTYs stay alive and
 *   continue to receive output in the background).
 * - Close (×) unmounts the XtermImpl → best-effort pty.close.
 * - Later: "Start agent ▾" dropdown can call ptyStartAgent and open a tab
 *   whose label reflects the agent name.
 */
export function TerminalTabs() {
  const [tabs, setTabs] = useState<Tab[]>([{ key: 1, label: "Terminal 1" }]);
  const [activeKey, setActiveKey] = useState<number>(1);
  const [nextKey, setNextKey] = useState<number>(2);

  const addTab = () => {
    if (tabs.length >= MAX_TABS) return;
    const k = nextKey;
    setTabs((prev) => [...prev, { key: k, label: `Terminal ${prev.length + 1}` }]);
    setActiveKey(k);
    setNextKey(k + 1);
  };

  const closeTab = (k: number, e?: React.MouseEvent) => {
    e?.stopPropagation();
    if (tabs.length <= 1) return;

    const newTabs = tabs.filter((t) => t.key !== k);
    setTabs(newTabs);

    if (activeKey === k) {
      setActiveKey(newTabs[0].key);
    }
  };

  const switchTab = (k: number) => setActiveKey(k);

  return (
    <div className="flex h-full w-full flex-col bg-zinc-950">
      {/* Tab bar */}
      <div className="flex items-center gap-1 border-b border-zinc-800 bg-zinc-950 px-2 py-1 text-xs text-zinc-400">
        {tabs.map((tab) => (
          <button
            key={tab.key}
            onClick={() => switchTab(tab.key)}
            className={cn(
              "flex items-center gap-1 rounded px-3 py-0.5 transition-colors",
              activeKey === tab.key
                ? "bg-zinc-800 text-zinc-100"
                : "hover:bg-zinc-800/60 hover:text-zinc-200"
            )}
            title={tab.label}
          >
            <span>{tab.label}</span>
            {tabs.length > 1 && (
              <span
                onClick={(e) => closeTab(tab.key, e)}
                className="ml-0.5 inline-block rounded px-1 text-[10px] opacity-60 hover:bg-zinc-700 hover:opacity-100"
              >
                ×
              </span>
            )}
          </button>
        ))}

        {tabs.length < MAX_TABS && (
          <button
            onClick={addTab}
            className="ml-1 rounded px-2 py-0.5 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
            title="New terminal (new PTY)"
          >
            +
          </button>
        )}

        {/* Future: agent launcher button / dropdown wired to ptyStartAgent */}
        {/* <button onClick={startClaude}>Spawn Claude</button> */}
      </div>

      {/* Terminal content area — all tabs mounted (so PTYs stay alive), only active visible */}
      <div className="relative min-h-0 flex-1 overflow-hidden">
        {tabs.map((tab) => (
          <div
            key={tab.key}
            className={cn(
              "absolute inset-0",
              activeKey !== tab.key && "hidden"
            )}
            data-pty-tab={tab.key}
          >
            <XtermImpl />
          </div>
        ))}
      </div>
    </div>
  );
}
