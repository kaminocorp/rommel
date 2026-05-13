"use client";

import Editor from "@monaco-editor/react";

// V1: no file wiring. Just a markdown welcome buffer that proves the bundle
// loads. Phase 6 adds fs.read on open, fs.write on save, tabs, dirty state.

const WELCOME = `# Welcome to Rommel

This editor pane is intentionally inert in Phase 5 — it proves the Monaco
bundle resolves end-to-end, including its workers, against the daemon's WS
plumbing in the same browser tab.

Phase 6 will:
- mount a file tree fed by \`fs.list\`
- open files via \`fs.read\` on click
- save via \`fs.write\`
- broadcast changes through \`fs.watch\` subscriptions
`;

export function MonacoImpl() {
  return (
    <Editor
      height="100%"
      defaultLanguage="markdown"
      defaultValue={WELCOME}
      theme="vs-dark"
      options={{
        minimap: { enabled: false },
        fontSize: 13,
        wordWrap: "on",
        scrollBeyondLastLine: false,
        smoothScrolling: true,
      }}
    />
  );
}
