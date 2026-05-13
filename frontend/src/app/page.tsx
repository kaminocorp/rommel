import Link from "next/link";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { getAccessTokenFromCookies } from "@/lib/auth";
import { api, ApiError } from "@/lib/api";
import type { Workspace } from "@/types/workspace";
import { WorkspaceCreateButton } from "@/components/shell/WorkspaceCreateButton";

// RSC. Fetches the user's workspace list with a Bearer token taken from
// the supabase-ssr cookie. Signed-out users are bounced (middleware handles
// /workspaces/*; the landing page does its own auth check for symmetry).

async function getWorkspaces(token: string): Promise<Workspace[]> {
  try {
    return await api<Workspace[]>("/workspaces", { token });
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return [];
    throw e;
  }
}

export default async function HomePage() {
  const cookieStore = await cookies();
  const token = await getAccessTokenFromCookies(cookieStore);
  if (!token) redirect("/sign-in?next=/");
  const workspaces = await getWorkspaces(token);

  return (
    <main className="mx-auto max-w-3xl px-6 py-12">
      <header className="mb-8 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Rommel</h1>
          <p className="text-sm text-zinc-400">Pick a workspace, or create a new one.</p>
        </div>
        <WorkspaceCreateButton />
      </header>

      {workspaces.length === 0 ? (
        <section className="rounded-lg border border-zinc-800 bg-zinc-900/40 p-6 text-zinc-300">
          <p className="text-sm">No workspaces yet. Hit “New workspace” to spin one up.</p>
        </section>
      ) : (
        <ul className="grid gap-3">
          {workspaces.map((w) => (
            <li key={w.id}>
              <Link
                href={`/workspaces/${w.id}`}
                className="block rounded-lg border border-zinc-800 bg-zinc-900/40 px-4 py-3 transition hover:border-zinc-600"
              >
                <div className="flex items-center justify-between">
                  <span className="font-medium">{w.name}</span>
                  <span className="text-xs text-zinc-500">{w.status}</span>
                </div>
                <div className="mt-1 text-xs text-zinc-500">{w.id}</div>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
