"use client";

import dynamic from "next/dynamic";

const TerminalImpl = dynamic(() => import("./xterm-impl").then((m) => m.XtermImpl), {
  ssr: false,
  loading: () => (
    <div className="flex h-full items-center justify-center text-xs text-zinc-500">
      Loading terminal…
    </div>
  ),
});

export function TerminalPane() {
  return <TerminalImpl />;
}
