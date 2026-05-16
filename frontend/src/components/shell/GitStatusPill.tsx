"use client";

import { useGitStatus } from "@/hooks/useGitStatus";

export function GitStatusPill() {
  const { data, isLoading, error } = useGitStatus();

  if (isLoading) {
    return <span className="text-zinc-500">git…</span>;
  }

  if (error || !data) {
    return null; // Not a git repo or error — hide the pill gracefully
  }

  const { branch, clean, ahead, behind } = data;

  if (!branch) {
    return null; // Not a git repo
  }

  const dirty = !clean;
  const tracking = ahead || behind ? `${ahead ? `↑${ahead}` : ""}${behind ? `↓${behind}` : ""}` : "";

  return (
    <div className="flex items-center gap-1 rounded border border-zinc-700 px-2 py-px text-[10px] tabular-nums">
      <span className={dirty ? "text-amber-400" : "text-emerald-400"}>●</span>
      <span>{branch}</span>
      {tracking && <span className="text-zinc-500">{tracking}</span>}
    </div>
  );
}
