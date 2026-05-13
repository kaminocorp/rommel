"use client";

// Monaco touches `window` on module-eval. `dynamic({ ssr: false })` keeps it
// off the server bundle entirely. Risk 4.1.

import dynamic from "next/dynamic";

const EditorImpl = dynamic(() => import("./monaco-impl").then((m) => m.MonacoImpl), {
  ssr: false,
  loading: () => (
    <div className="flex h-full items-center justify-center text-xs text-zinc-500">
      Loading editor…
    </div>
  ),
});

export function EditorPane() {
  return <EditorImpl />;
}
