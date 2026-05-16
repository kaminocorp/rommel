"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  FsDeleteResponse,
  FsListResponse,
  FsMkdirResponse,
  FsMoveResponse,
  FsReadResponse,
  FsWriteResponse,
} from "@rommel/proto";
import { fsDelete, fsList, fsMkdir, fsMove, fsRead, fsWrite } from "@/lib/fs";
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
      void qc.invalidateQueries({ queryKey: ["fs", "read", path] });
      void qc.invalidateQueries({ queryKey: ["fs", "list"] });
    },
  });
}

export function useFsMkdir() {
  const daemon = useReadyDaemon();
  const qc = useQueryClient();
  return useMutation<FsMkdirResponse, Error, { path: string; recursive?: boolean }>({
    mutationFn: ({ path, recursive }) => fsMkdir(daemon!, path, recursive ?? false),
    onSuccess: (_resp, { path }) => {
      // Invalidate the parent list so the new directory appears in the tree.
      const parent = path.includes("/") ? path.substring(0, path.lastIndexOf("/")) || "." : ".";
      void qc.invalidateQueries({ queryKey: ["fs", "list", parent] });
      void qc.invalidateQueries({ queryKey: ["fs", "list"] });
    },
  });
}

export function useFsMove() {
  const daemon = useReadyDaemon();
  const qc = useQueryClient();
  return useMutation<FsMoveResponse, Error, { from: string; to: string }>({
    mutationFn: ({ from, to }) => fsMove(daemon!, from, to),
    onSuccess: (_resp, { from, to }) => {
      const fromParent = from.includes("/") ? from.substring(0, from.lastIndexOf("/")) || "." : ".";
      const toParent = to.includes("/") ? to.substring(0, to.lastIndexOf("/")) || "." : ".";
      void qc.invalidateQueries({ queryKey: ["fs", "list", fromParent] });
      void qc.invalidateQueries({ queryKey: ["fs", "list", toParent] });
      void qc.invalidateQueries({ queryKey: ["fs", "list"] });
      void qc.invalidateQueries({ queryKey: ["fs", "read", from] });
    },
  });
}

export function useFsDelete() {
  const daemon = useReadyDaemon();
  const qc = useQueryClient();
  return useMutation<FsDeleteResponse, Error, { path: string; recursive?: boolean }>({
    mutationFn: ({ path, recursive }) => fsDelete(daemon!, path, recursive ?? false),
    onSuccess: (_resp, { path }) => {
      const parent = path.includes("/") ? path.substring(0, path.lastIndexOf("/")) || "." : ".";
      void qc.invalidateQueries({ queryKey: ["fs", "list", parent] });
      void qc.invalidateQueries({ queryKey: ["fs", "list"] });
      void qc.invalidateQueries({ queryKey: ["fs", "read", path] });
    },
  });
}
