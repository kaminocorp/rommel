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
import { ChevronDown, ChevronRight, Edit2, FileIcon, FolderIcon, Plus, Trash2 } from "lucide-react";
import type { FsListEntry } from "@rommel/proto";
import { useFsDelete, useFsList, useFsMkdir, useFsMove } from "@/hooks/useFs";
import { useConnectionStore } from "@/stores/connection";
import { cn } from "@/lib/utils";

export function FileTree() {
  const [menu, setMenu] = useState<{
    x: number;
    y: number;
    targetPath: string;
    targetKind: "file" | "dir";
    parentPath: string;
  } | null>(null);

  const mkdir = useFsMkdir();
  const move = useFsMove();
  const del = useFsDelete();

  const closeMenu = () => setMenu(null);

  // "New File" goes through createEmptyFile (below) so it can grab the daemon
  // from the connection store directly. "New Folder" goes through the mkdir
  // mutation, which the hook already wires to the daemon.
  const handleNewDir = async (parent: string) => {
    closeMenu();
    const name = window.prompt("New folder name:", "New Folder");
    if (!name) return;
    const newPath = parent === "." ? name : `${parent}/${name}`;
    try {
      await mkdir.mutateAsync({ path: newPath, recursive: false });
    } catch (e: any) {
      alert("Failed: " + (e?.message ?? e));
    }
  };

  const handleRename = async (targetPath: string) => {
    closeMenu();
    const current = targetPath.split("/").pop() || targetPath;
    const name = window.prompt("Rename to:", current);
    if (!name || name === current) return;
    const parent = targetPath.includes("/") ? targetPath.substring(0, targetPath.lastIndexOf("/")) : ".";
    const newPath = parent === "." ? name : `${parent}/${name}`;
    try {
      await move.mutateAsync({ from: targetPath, to: newPath });
    } catch (e: any) {
      alert("Rename failed: " + (e?.message ?? e));
    }
  };

  const handleDelete = async (targetPath: string, kind: "file" | "dir") => {
    closeMenu();
    if (!confirm(`Delete ${kind} "${targetPath}"?`)) return;
    const recursive = kind === "dir" && confirm("Delete recursively (all contents)?");
    try {
      await del.mutateAsync({ path: targetPath, recursive });
    } catch (e: any) {
      alert("Delete failed: " + (e?.message ?? e));
    }
  };

  // Helper to create empty file (uses the connection store daemon directly)
  const createEmptyFile = async (parent: string) => {
    const name = window.prompt("New file name:", "untitled.txt");
    if (!name) return;
    const newPath = parent === "." ? name : `${parent}/${name}`;
    // We import dynamically to avoid circular issues in this edit
    const { fsWrite } = await import("@/lib/fs");
    const daemon = useConnectionStore.getState().daemon;
    if (!daemon) {
      alert("Not connected");
      return;
    }
    try {
      await fsWrite(daemon, newPath, "", "utf-8");
    } catch (e: any) {
      alert("Create file failed: " + (e?.message ?? e));
    }
  };

  return (
    <div className="p-2 text-xs" onClick={closeMenu}>
      <div className="mb-2 flex items-center justify-between px-1">
        <p className="font-medium uppercase tracking-wide text-zinc-500">Files</p>
        <button
          onClick={(e) => {
            e.stopPropagation();
            createEmptyFile(".");
          }}
          className="rounded p-1 hover:bg-zinc-800"
          title="New file in root"
        >
          <Plus className="size-3.5" />
        </button>
      </div>

      <Node
        path="."
        name="(workspace root)"
        depth={0}
        initiallyOpen
        onContextMenu={(e, targetPath, kind, parentPath) => {
          setMenu({
            x: e.clientX,
            y: e.clientY,
            targetPath,
            targetKind: kind,
            parentPath,
          });
        }}
      />

      {/* Context Menu */}
      {menu && (
        <div
          className="fixed z-50 min-w-[160px] rounded border border-zinc-700 bg-zinc-900 py-1 text-xs shadow-xl"
          style={{ left: menu.x, top: menu.y }}
          onClick={(e) => e.stopPropagation()}
        >
          {menu.targetKind === "dir" && (
            <>
              <MenuItem icon={Plus} label="New File" onClick={() => createEmptyFile(menu.targetPath)} />
              <MenuItem icon={Plus} label="New Folder" onClick={() => handleNewDir(menu.targetPath)} />
              <div className="my-1 border-t border-zinc-700" />
            </>
          )}
          <MenuItem icon={Edit2} label="Rename" onClick={() => handleRename(menu.targetPath)} />
          <MenuItem
            icon={Trash2}
            label="Delete"
            destructive
            onClick={() => handleDelete(menu.targetPath, menu.targetKind)}
          />
        </div>
      )}
    </div>
  );
}

function MenuItem({
  icon: Icon,
  label,
  onClick,
  destructive,
}: {
  icon: any;
  label: string;
  onClick: () => void;
  destructive?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-zinc-800",
        destructive && "text-red-400 hover:text-red-300"
      )}
    >
      <Icon className="size-3.5" />
      {label}
    </button>
  );
}


type NodeProps = {
  path: string;
  name: string;
  depth: number;
  initiallyOpen?: boolean;
  onContextMenu?: (e: React.MouseEvent, targetPath: string, kind: "file" | "dir", parentPath: string) => void;
};

function Node({ path, name, depth, initiallyOpen = false, onContextMenu }: NodeProps) {
  const [open, setOpen] = useState(initiallyOpen);
  // Only fetch when expanded — collapsed directories are inert.
  const { data, isLoading, isError, error } = useFsList(path, { enabled: open });

  const handleContextMenu = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    onContextMenu?.(e, path, "dir", path === "." ? "." : path.substring(0, path.lastIndexOf("/")) || ".");
  };

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        onContextMenu={handleContextMenu}
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
            <Entry
              key={entry.name}
              entry={entry}
              parentPath={path}
              depth={depth + 1}
              onContextMenu={onContextMenu}
            />
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
  onContextMenu,
}: {
  entry: FsListEntry;
  parentPath: string;
  depth: number;
  onContextMenu?: NodeProps["onContextMenu"];
}) {
  const childPath = parentPath === "." ? entry.name : `${parentPath}/${entry.name}`;
  if (entry.kind === "dir") {
    return <Node path={childPath} name={entry.name} depth={depth} onContextMenu={onContextMenu} />;
  }
  // file or symlink (symlinks rendered as files in v1)
  return <FileEntry path={childPath} name={entry.name} depth={depth} onContextMenu={onContextMenu} />;
}

function FileEntry({
  path,
  name,
  depth,
  onContextMenu,
}: {
  path: string;
  name: string;
  depth: number;
  onContextMenu?: NodeProps["onContextMenu"];
}) {
  const selectedFile = useConnectionStore((s) => s.selectedFile);
  const selectFile = useConnectionStore((s) => s.selectFile);
  const isSelected = selectedFile === path;

  const handleContextMenu = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const parent = path.includes("/") ? path.substring(0, path.lastIndexOf("/")) : ".";
    onContextMenu?.(e, path, "file", parent);
  };

  return (
    <button
      type="button"
      onClick={() => selectFile(path)}
      onContextMenu={handleContextMenu}
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
