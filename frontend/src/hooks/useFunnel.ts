"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  FunnelListResponse,
  FunnelPromoteResponse,
  FunnelReadResponse,
  FunnelStage,
} from "@rommel/proto";
import { funnelList, funnelPromote, funnelRead } from "@/lib/funnel";
import { useConnectionStore } from "@/stores/connection";

function useReadyDaemon() {
  return useConnectionStore((s) => (s.status === "ready" ? s.daemon : null));
}

export function useFunnelList(stage: FunnelStage) {
  const daemon = useReadyDaemon();
  return useQuery<FunnelListResponse>({
    queryKey: ["funnel", "list", stage],
    queryFn: () => funnelList(daemon!, stage),
    enabled: !!daemon,
    // The funnel UI is a passive read; aggressive refetch isn't useful here.
    staleTime: 10_000,
  });
}

export function useFunnelRead(stage: FunnelStage, name: string | null) {
  const daemon = useReadyDaemon();
  return useQuery<FunnelReadResponse>({
    queryKey: ["funnel", "read", stage, name],
    queryFn: () => funnelRead(daemon!, stage, name!),
    enabled: !!daemon && !!name,
    staleTime: 0,
  });
}

export function useFunnelPromote() {
  const daemon = useReadyDaemon();
  const qc = useQueryClient();
  return useMutation<
    FunnelPromoteResponse,
    Error,
    { name: string; from: FunnelStage; to: FunnelStage }
  >({
    mutationFn: ({ name, from, to }) => funnelPromote(daemon!, name, from, to),
    onSuccess: (_resp, { from, to }) => {
      void qc.invalidateQueries({ queryKey: ["funnel", "list", from] });
      void qc.invalidateQueries({ queryKey: ["funnel", "list", to] });
    },
  });
}
