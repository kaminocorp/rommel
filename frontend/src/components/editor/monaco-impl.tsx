"use client";

import Editor, { type OnMount } from "@monaco-editor/react";
import { useEffect, useRef, useState } from "react";
import { useConnectionStore } from "@/stores/connection";
import { useFsRead, useFsWrite } from "@/hooks/useFs";

// Phase 6: real file open/save.
//
// - selectedFile (from store) → fs.read → load into Monaco
// - Cmd/Ctrl+S → fs.write with the current buffer
// - dirty-state tracking; clean when buffer matches the last fs.read/fs.write
//
// Tabs and a dirty-state confirmation modal are deferred — clicking another
// file in the tree replaces the buffer outright. Save is an explicit act.

const WELCOME = `# Welcome to Rommel

Open a file from the tree on the left.

- \`Cmd/Ctrl+S\` saves via \`fs.write\`.
- Switching files discards unsaved edits in v1 (no confirm modal yet).
- The funnel board on the right of the header switches into the kanban view.
`;

// Tiny extension → Monaco language inference. Covers the obvious cases; the
// rest fall back to "plaintext", which Monaco renders fine.
function languageForPath(path: string | null): string {
  if (!path) return "markdown";
  const ext = path.toLowerCase().split(".").pop() ?? "";
  switch (ext) {
    case "ts":
    case "tsx":
      return "typescript";
    case "js":
    case "jsx":
      return "javascript";
    case "json":
      return "json";
    case "md":
    case "mdx":
      return "markdown";
    case "py":
      return "python";
    case "go":
      return "go";
    case "yml":
    case "yaml":
      return "yaml";
    case "sh":
    case "bash":
      return "shell";
    case "css":
      return "css";
    case "html":
      return "html";
    case "rs":
      return "rust";
    case "toml":
      return "ini";
    default:
      return "plaintext";
  }
}

export function MonacoImpl() {
  const selectedFile = useConnectionStore((s) => s.selectedFile);
  const { data, isLoading, isError, error } = useFsRead(selectedFile);
  const { mutateAsync: writeFile, isPending: saving } = useFsWrite();

  const [buffer, setBuffer] = useState<string>(WELCOME);
  const [lastSavedAt, setLastSavedAt] = useState<number | null>(null);
  const [dirty, setDirty] = useState(false);
  const editorRef = useRef<Parameters<OnMount>[0] | null>(null);

  // When the read resolves (or selection changes), reset the buffer.
  useEffect(() => {
    if (!selectedFile) {
      setBuffer(WELCOME);
      setDirty(false);
      return;
    }
    if (data) {
      setBuffer(data.contents);
      setDirty(false);
    }
  }, [selectedFile, data]);

  const handleMount: OnMount = (editor, monaco) => {
    editorRef.current = editor;
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
      void save();
    });
  };

  async function save() {
    if (!selectedFile) return; // welcome buffer — no save target
    const contents = editorRef.current?.getValue() ?? buffer;
    try {
      await writeFile({ path: selectedFile, contents });
      setBuffer(contents);
      setDirty(false);
      setLastSavedAt(Date.now());
    } catch (e) {
      // Surface in the title for now; toast layer comes later.
      console.error("fs.write failed", e);
    }
  }

  const titlePath = selectedFile ?? "Welcome";
  const status = (() => {
    if (!selectedFile) return "";
    if (isLoading) return "loading…";
    if (isError) return `error: ${(error as Error)?.message ?? "unknown"}`;
    if (saving) return "saving…";
    if (dirty) return "● modified";
    if (lastSavedAt) return `saved ${secondsAgo(lastSavedAt)}s ago`;
    return "";
  })();

  return (
    <div className="flex h-full flex-col">
      <div
        data-testid="editor-titlebar"
        data-dirty={dirty ? "true" : "false"}
        className="flex items-center justify-between border-b border-zinc-800 bg-zinc-900/40 px-3 py-1 text-xs text-zinc-400"
      >
        <span className="truncate font-mono">{titlePath}</span>
        <span className="text-zinc-500">{status}</span>
      </div>
      <div className="min-h-0 flex-1">
        <Editor
          height="100%"
          path={selectedFile ?? "welcome.md"}
          language={languageForPath(selectedFile)}
          value={buffer}
          theme="vs-dark"
          onChange={(v) => {
            const next = v ?? "";
            setBuffer(next);
            setDirty(true);
          }}
          onMount={handleMount}
          options={{
            minimap: { enabled: false },
            fontSize: 13,
            wordWrap: "on",
            scrollBeyondLastLine: false,
            smoothScrolling: true,
            readOnly: !!selectedFile && isLoading,
          }}
        />
      </div>
    </div>
  );
}

function secondsAgo(ts: number): number {
  return Math.max(0, Math.round((Date.now() - ts) / 1000));
}
