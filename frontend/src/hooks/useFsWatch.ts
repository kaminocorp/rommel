"use client";

import { useEffect, useRef, useState } from "react";
import type { FsWatchEvent } from "@rommel/proto";
import { fsWatch } from "@/lib/fs";
import { useConnectionStore } from "@/stores/connection";

export type FsWatchStatus = "idle" | "watching" | "error";

export type UseFsWatchOptions = {
  path: string;
  recursive?: boolean;
  // Called for every fs.watch-event whose path is under (or equal to) the
  // watched root. The consumer decides what to do (e.g. invalidate a TanStack
  // Query key for an open editor file, or refresh a FileTree subtree).
  onEvent?: (event: FsWatchEvent) => void;
};

export type UseFsWatchResult = {
  status: FsWatchStatus;
  error: string | null;
  // Last raw event (useful for debugging or simple consumers).
  lastEvent: FsWatchEvent | null;
};

/**
 * useFsWatch starts an fs.watch subscription for a path when the daemon
 * connection is ready. It wires the subscribe() seam and cleans up on unmount.
 *
 * The daemon automatically tears down the watch when the WS connection drops
 * (via the fs.Handler's OnDisconnect implementation).
 *
 * Typical usage in an editor:
 *   useFsWatch({
 *     path: dirname(selectedFile),
 *     onEvent: (e) => {
 *       if (e.path === selectedFile && (e.type === "modified" || e.type === "deleted")) {
 *         qc.invalidateQueries({ queryKey: ["fs", "read", selectedFile] });
 *       }
 *     },
 *   });
 */
export function useFsWatch(opts: UseFsWatchOptions): UseFsWatchResult {
  const daemon = useConnectionStore((s) => (s.status === "ready" ? s.daemon : null));
  const [status, setStatus] = useState<FsWatchStatus>("idle");
  const [error, setError] = useState<string | null>(null);
  const [lastEvent, setLastEvent] = useState<FsWatchEvent | null>(null);

  const onEventRef = useRef(opts.onEvent);
  onEventRef.current = opts.onEvent;

  const pathRef = useRef(opts.path);
  const recursiveRef = useRef(opts.recursive);

  useEffect(() => {
    pathRef.current = opts.path;
    recursiveRef.current = opts.recursive;
  }, [opts.path, opts.recursive]);

  useEffect(() => {
    if (!daemon) {
      setStatus("idle");
      return;
    }

    let cancelled = false;
    let unsub: (() => void) | null = null;

    (async () => {
      try {
        setStatus("watching");
        setError(null);

        // Start the watch on the daemon side. The response is just an ack;
        // real events arrive via the "fs.watch-event" subscription.
        await fsWatch(daemon, pathRef.current, recursiveRef.current);

        if (cancelled) return;

        unsub = daemon.subscribe("fs.watch-event", (payload: unknown) => {
          // The payload is the raw JSON from the event envelope.
          // We rely on the generated FsWatchEvent type for consumers.
          const ev = payload as FsWatchEvent;
          setLastEvent(ev);
          onEventRef.current?.(ev);
        });
      } catch (e) {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
        setStatus("error");
      }
    })();

    return () => {
      cancelled = true;
      unsub?.();
      // Best-effort: we could send an explicit "fs.unwatch" here in a future
      // revision. For v1 the daemon's OnDisconnect is the safety net.
    };
  }, [daemon]);

  return { status, error, lastEvent };
}
