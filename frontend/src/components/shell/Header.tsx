"use client";

import Link from "next/link";
import { useMe } from "@/hooks/useWorkspace";
import type { Workspace } from "@/types/workspace";

export function Header({ workspace }: { workspace: Workspace }) {
  const me = useMe();

  return (
    <header className="flex h-12 items-center justify-between border-b border-zinc-800 bg-zinc-950 px-4">
      <div className="flex items-center gap-3">
        <Link href="/" className="text-sm font-medium text-zinc-300 hover:text-zinc-100">
          Rommel
        </Link>
        <span className="text-zinc-600">/</span>
        <span className="text-sm text-zinc-200">{workspace.name}</span>
        <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-zinc-400">
          {workspace.status}
        </span>
      </div>
      <div className="text-xs text-zinc-500">
        {me.data?.email ?? me.data?.sub ?? "…"}
      </div>
    </header>
  );
}
