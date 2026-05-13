import { cookies } from "next/headers";
import { notFound } from "next/navigation";
import { getAccessTokenFromCookies } from "@/lib/auth";
import { api, ApiError } from "@/lib/api";
import type { Workspace } from "@/types/workspace";
import { WorkspaceClient } from "./workspace-client";

type Params = { id: string };

export default async function WorkspacePage({ params }: { params: Promise<Params> }) {
  const { id } = await params;
  const cookieStore = await cookies();
  const token = await getAccessTokenFromCookies(cookieStore);
  // Middleware should have already redirected unauthenticated users; if a
  // token is somehow missing here, fall through to the standard not-found
  // rather than leaking implementation details.
  if (!token) notFound();

  let workspace: Workspace;
  try {
    workspace = await api<Workspace>(`/workspaces/${id}`, { token });
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  return <WorkspaceClient workspace={workspace} />;
}
