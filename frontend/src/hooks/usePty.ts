"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { PtyExitEvent, PtyOutputDroppedEvent, PtyOutputEvent } from "@rommel/proto";
import { base64ToBytes, ptyClose, ptyInput, ptyOpen, ptyResize } from "@/lib/pty";
import { useConnectionStore } from "@/stores/connection";

// usePty owns the lifecycle of a single PTY:
//   - opens on mount once the DaemonConnection is ready
//   - subscribes to pty.output / pty.exit / pty.output_dropped, filtered by
//     the pty_id minted at open time
//   - closes on unmount (best-effort; the daemon also cleans up via
//     OnDisconnect if the socket drops without us sending pty.close)
//
// API shape: a callback for output (the component pipes straight into xterm,
// no buffering at the hook layer) + imperative send/resize methods. Status
// + exit code are React state so the title strip can render them.

export type PtyStatus = "opening" | "ready" | "exited" | "error";

export type UsePtyOptions = {
  cols: number;
  rows: number;
  cwd?: string;
  env?: Record<string, string>;
  // Output stream. Bytes are passed through verbatim — xterm.js accepts
  // Uint8Array directly and runs its own UTF-8 / escape-sequence decoder.
  onOutput?: (bytes: Uint8Array) => void;
  // Fired exactly once when the shell exits. signal is populated for
  // signal-terminated processes (SIGTERM/SIGKILL/etc).
  onExit?: (event: { exitCode: number; signal?: string }) => void;
};

export type UsePtyResult = {
  ptyId: string | null;
  status: PtyStatus;
  exitCode: number | null;
  signal: string | null;
  droppedCount: number;
  error: string | null;
  send: (data: Uint8Array | string) => void;
  resize: (cols: number, rows: number) => void;
};

function useReadyDaemon() {
  return useConnectionStore((s) => (s.status === "ready" ? s.daemon : null));
}

export function usePty(opts: UsePtyOptions): UsePtyResult {
  const daemon = useReadyDaemon();
  const [ptyId, setPtyId] = useState<string | null>(null);
  const [status, setStatus] = useState<PtyStatus>("opening");
  const [exitCode, setExitCode] = useState<number | null>(null);
  const [signal, setSignal] = useState<string | null>(null);
  const [droppedCount, setDroppedCount] = useState(0);
  const [error, setError] = useState<string | null>(null);

  // Stable refs so the open effect doesn't tear down on every callback
  // identity change. Components passing inline arrow fns are a footgun
  // otherwise — the PTY would be re-opened on every render.
  const onOutputRef = useRef(opts.onOutput);
  const onExitRef = useRef(opts.onExit);
  onOutputRef.current = opts.onOutput;
  onExitRef.current = opts.onExit;

  // Initial cols/rows are captured at open time. Subsequent changes go
  // through resize(); we don't re-open on dimension change.
  const initialColsRef = useRef(opts.cols);
  const initialRowsRef = useRef(opts.rows);
  const cwdRef = useRef(opts.cwd);
  const envRef = useRef(opts.env);

  useEffect(() => {
    if (!daemon) return;
    let cancelled = false;
    let localPtyId: string | null = null;
    const unsubs: Array<() => void> = [];

    (async () => {
      try {
        const resp = await ptyOpen(daemon, {
          cols: initialColsRef.current,
          rows: initialRowsRef.current,
          ...(cwdRef.current !== undefined ? { cwd: cwdRef.current } : {}),
          ...(envRef.current !== undefined ? { env: envRef.current } : {}),
        });
        if (cancelled) {
          // Race: hook unmounted while the open was in flight. Tell the
          // daemon to clean up the orphan rather than leaking the shell.
          void ptyClose(daemon, resp.pty_id).catch(() => {});
          return;
        }
        localPtyId = resp.pty_id;
        setPtyId(resp.pty_id);
        setStatus("ready");

        unsubs.push(
          daemon.subscribe("pty.output", (payload) => {
            const ev = payload as PtyOutputEvent;
            if (ev.pty_id !== resp.pty_id) return;
            onOutputRef.current?.(base64ToBytes(ev.data));
          }),
        );
        unsubs.push(
          daemon.subscribe("pty.exit", (payload) => {
            const ev = payload as PtyExitEvent;
            if (ev.pty_id !== resp.pty_id) return;
            setExitCode(ev.exit_code);
            setSignal(ev.signal ?? null);
            setStatus("exited");
            onExitRef.current?.({
              exitCode: ev.exit_code,
              ...(ev.signal !== undefined ? { signal: ev.signal } : {}),
            });
          }),
        );
        unsubs.push(
          daemon.subscribe("pty.output_dropped", (payload) => {
            const ev = payload as PtyOutputDroppedEvent;
            if (ev.pty_id !== resp.pty_id) return;
            setDroppedCount((d) => d + ev.dropped_count);
          }),
        );
      } catch (e) {
        if (cancelled) return;
        setStatus("error");
        setError(e instanceof Error ? e.message : String(e));
      }
    })();

    return () => {
      cancelled = true;
      for (const u of unsubs) u();
      // Best-effort close. If the daemon has already torn the PTY down
      // (e.g. shell exited naturally), pty.close is idempotent.
      if (localPtyId) {
        void ptyClose(daemon, localPtyId).catch(() => {});
      }
    };
  }, [daemon]);

  const send = useCallback(
    (data: Uint8Array | string) => {
      if (!daemon || !ptyId) return;
      ptyInput(daemon, ptyId, data);
    },
    [daemon, ptyId],
  );

  const resize = useCallback(
    (cols: number, rows: number) => {
      if (!daemon || !ptyId) return;
      void ptyResize(daemon, ptyId, cols, rows).catch(() => {
        // Resize failures aren't user-actionable. The PTY keeps the prior
        // size; the UI may render slightly off until the next resize.
      });
    },
    [daemon, ptyId],
  );

  return { ptyId, status, exitCode, signal, droppedCount, error, send, resize };
}
