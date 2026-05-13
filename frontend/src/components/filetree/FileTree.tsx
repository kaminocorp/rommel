"use client";

// fs.list-driven file tree. Phase 6.
//
// v1 shape: a single recursive component. Each `Node` mounts its own
// useFsList(path) query when expanded. Clicking a file pokes selectedFile in
// the connection store; the EditorPane picks it up and runs fs.read.
//
// Deliberately non-virtualized: workspaces with >1k entries per directory
// will get sluggish — virtualization is a follow-up when there's a workspace
// big enough to need it.

import { useState } from "react";
import { ChevronDown, ChevronRight, FileIcon, FolderIcon } from "lucide-react";
import type { FsListEntry } from "@rommel/proto";
import { useFsList } from "@/hooks/useFs";
import { useConnectionStore } from "@/stores/connection";
import { cn } from "@/lib/utils";

export function FileTree() {
  return (
    <div className="p-2 text-xs">
      <p className="mb-2 px-1 font-medium uppercase tracking-wide text-zinc-500">Files</p>
      <Node path="." name="(workspace root)" depth={0} initiallyOpen />
    </div>
  );
}

type NodeProps = {
  path: string;
  name: string;
  depth: number;
  initiallyOpen?: boolean;
};

function Node({ path, name, depth, initiallyOpen = false }: NodeProps) {
  const [open, setOpen] = useState(initiallyOpen);
  // Only fetch when expanded — collapsed directories are inert.
  const { data, isLoading, isError, error } = useFsList(path, { enabled: open });

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        data-testid="filetree-dir"
        className={cn(
          "flex w-full items-center gap-1 rounded px-1 py-0.5 text-left hover:bg-zinc-800/50",
          depth === 0 && "text-zinc-400",
        )}
        style={{ paddingLeft: `${depth * 0.75 + 0.25}rem` }}
      >
        {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
        <FolderIcon className="size-3.5 text-zinc-500" />
        <span className="truncate">{name}</span>
      </button>
      {open && (
        <div>
          {isLoading && <Hint depth={depth}>loading…</Hint>}
          {isError && <Hint depth={depth}>error: {(error as Error)?.message ?? "unknown"}</Hint>}
          {data?.entries?.map((entry) => (
            <Entry key={entry.name} entry={entry} parentPath={path} depth={depth + 1} />
          ))}
          {data && data.entries.length === 0 && <Hint depth={depth}>(empty)</Hint>}
        </div>
      )}
    </div>
  );
}

function Entry({
  entry,
  parentPath,
  depth,
}: {
  entry: FsListEntry;
  parentPath: string;
  depth: number;
}) {
  const childPath = parentPath === "." ? entry.name : `${parentPath}/${entry.name}`;
  if (entry.kind === "dir") {
    return <Node path={childPath} name={entry.name} depth={depth} />;
  }
  // file or symlink (symlinks rendered as files in v1)
  return <FileEntry path={childPath} name={entry.name} depth={depth} />;
}

function FileEntry({ path, name, depth }: { path: string; name: string; depth: number }) {
  const selectedFile = useConnectionStore((s) => s.selectedFile);
  const selectFile = useConnectionStore((s) => s.selectFile);
  const isSelected = selectedFile === path;
  return (
    <button
      type="button"
      onClick={() => selectFile(path)}
      data-testid="filetree-file"
      data-path={path}
      className={cn(
        "flex w-full items-center gap-1 rounded px-1 py-0.5 text-left hover:bg-zinc-800/50",
        isSelected ? "bg-zinc-800 text-zinc-100" : "text-zinc-400",
      )}
      style={{ paddingLeft: `${depth * 0.75 + 0.75}rem` }}
    >
      <FileIcon className="size-3.5 text-zinc-500" />
      <span className="truncate">{name}</span>
    </button>
  );
}

function Hint({ children, depth }: { children: React.ReactNode; depth: number }) {
  return (
    <div
      className="py-0.5 text-zinc-600"
      style={{ paddingLeft: `${depth * 0.75 + 1.5}rem` }}
    >
      {children}
    </div>
  );
}
