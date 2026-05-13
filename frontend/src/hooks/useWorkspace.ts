"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { createBrowserClient } from "@/lib/auth";
import type { SessionResponse, UserClaims, Workspace } from "@/types/workspace";

// All three hooks need the JWT to attach Authorization: Bearer. We pull it
// from the supabase-ssr browser client per request — cookies are the source
// of truth, the client is a thin reader.

async function getToken(): Promise<string | null> {
  const supabase = createBrowserClient();
  const { data } = await supabase.auth.getSession();
  return data.session?.access_token ?? null;
}

export function useMe() {
  return useQuery<UserClaims>({
    queryKey: ["auth", "me"],
    queryFn: async () => {
      const token = await getToken();
      return api<UserClaims>("/auth/me", { token });
    },
  });
}

export function useWorkspaces() {
  return useQuery<Workspace[]>({
    queryKey: ["workspaces"],
    queryFn: async () => {
      const token = await getToken();
      return api<Workspace[]>("/workspaces", { token });
    },
  });
}

export function useWorkspace(id: string) {
  return useQuery<Workspace>({
    queryKey: ["workspaces", id],
    queryFn: async () => {
      const token = await getToken();
      return api<Workspace>(`/workspaces/${id}`, { token });
    },
    enabled: !!id,
  });
}

export function useCreateWorkspace() {
  const qc = useQueryClient();
  return useMutation<Workspace, Error, { name: string }>({
    mutationFn: async ({ name }) => {
      const token = await getToken();
      return api<Workspace>("/workspaces", {
        method: "POST",
        token,
        body: JSON.stringify({ name }),
      });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspaces"] }),
  });
}

export function useCreateSession(workspaceId: string) {
  return useMutation<SessionResponse, Error, void>({
    mutationFn: async () => {
      const token = await getToken();
      return api<SessionResponse>(`/workspaces/${workspaceId}/sessions`, {
        method: "POST",
        token,
      });
    },
  });
}
