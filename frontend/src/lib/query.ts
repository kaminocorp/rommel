"use client";

import { QueryClient } from "@tanstack/react-query";

// One QueryClient per request on the server, one per browser tab on the client.
// `staleTime: 30s` so the workspace list doesn't re-fetch on every tab switch.

let browserClient: QueryClient | undefined;

function make(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        gcTime: 5 * 60_000,
        retry: (failureCount, error) => {
          // Don't retry auth failures — let middleware bounce the user.
          if (error instanceof Error && /^API 401/.test(error.message)) return false;
          return failureCount < 2;
        },
        refetchOnWindowFocus: false,
      },
    },
  });
}

export function getQueryClient(): QueryClient {
  if (typeof window === "undefined") return make();
  if (!browserClient) browserClient = make();
  return browserClient;
}
