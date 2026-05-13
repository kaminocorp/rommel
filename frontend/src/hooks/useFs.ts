"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { FsListResponse, FsReadResponse, FsWriteResponse } from "@rommel/proto";
import { fsList, fsRead, fsWrite } from "@/lib/fs";
import { useConnectionStore } from "@/stores/connection";

// TanStack Query wrappers for the fs.* primitives. Each hook reads the
// shared DaemonConnection from the connection store. Hooks are `enabled`
// only when the connection is non-null and `status === "ready"` so we don't
// fire RPCs against a half-built socket.

function useReadyDaemon() {
  return useConnectionStore((s) => (s.status === "ready" ? s.daemon : null));
}

export function useFsList(path: string, opts: { enabled?: boolean } = {}) {
  const daemon = useReadyDaemon();
  const enabled = (opts.enabled ?? true) && !!daemon;
  return useQuery<FsListResponse>({
    queryKey: ["fs", "list", path],
    queryFn: () => fsList(daemon!, path),
    enabled,
    staleTime: 5_000,
  });
}

export function useFsRead(path: string | null) {
  const daemon = useReadyDaemon();
  return useQuery<FsReadResponse>({
    queryKey: ["fs", "read", path],
    queryFn: () => fsRead(daemon!, path!),
    enabled: !!daemon && !!path,
    staleTime: 0,
  });
}

export function useFsWrite() {
  const daemon = useReadyDaemon();
  const qc = useQueryClient();
  return useMutation<FsWriteResponse, Error, { path: string; contents: string }>({
    mutationFn: ({ path, contents }) => fsWrite(daemon!, path, contents),
    onSuccess: (_resp, { path }) => {
      // Invalidate the read of this specific path and any list that might
      // include it. Listing keys are keyed by directory, so blanket-
      // invalidating the "fs", "list" prefix is the simplest correct move.
      void qc.invalidateQueries({ queryKey: ["fs", "read", path] });
      void qc.invalidateQueries({ queryKey: ["fs", "list"] });
    },
  });
}
