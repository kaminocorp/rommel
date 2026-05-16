"use client";

import { useQuery } from "@tanstack/react-query";
import { gitStatus } from "@/lib/git";
import { useConnectionStore } from "@/stores/connection";

export function useGitStatus(path?: string) {
  const daemon = useConnectionStore((s) => (s.status === "ready" ? s.daemon : null));

  return useQuery({
    queryKey: ["git", "status", path],
    queryFn: () => gitStatus(daemon!, path),
    enabled: !!daemon,
    staleTime: 10_000, // 10 seconds is plenty for a status pill
    refetchOnWindowFocus: false,
  });
}
