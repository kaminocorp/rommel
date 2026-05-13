"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useCreateWorkspace } from "@/hooks/useWorkspace";
import { Button } from "@/components/ui/button";

export function WorkspaceCreateButton() {
  const router = useRouter();
  const create = useCreateWorkspace();
  const [name, setName] = useState("");
  const [showForm, setShowForm] = useState(false);

  if (!showForm) {
    return <Button onClick={() => setShowForm(true)}>New workspace</Button>;
  }

  return (
    <form
      className="flex items-center gap-2"
      onSubmit={async (e) => {
        e.preventDefault();
        const trimmed = name.trim() || "untitled";
        const ws = await create.mutateAsync({ name: trimmed });
        router.push(`/workspaces/${ws.id}`);
      }}
    >
      <input
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="workspace name"
        className="h-9 rounded-md border border-zinc-700 bg-zinc-900 px-3 text-sm text-zinc-100 outline-none focus:border-zinc-500"
      />
      <Button type="submit" disabled={create.isPending}>
        {create.isPending ? "Creating…" : "Create"}
      </Button>
      <Button type="button" variant="ghost" onClick={() => setShowForm(false)}>
        Cancel
      </Button>
    </form>
  );
}
