"use client";

import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { usePty } from "@/hooks/usePty";

// PTY-wired terminal pane. Phase 7 — mounts xterm, opens a PTY against the
// daemon, pipes keystrokes → pty.input and pty.output → xterm.write.
//
// The terminal's `disableStdin` is now false: every keystroke is forwarded
// to the shell. ResizeObserver fires fit.fit() then propagates the new
// cols/rows to the daemon via pty.resize (debounced 150 ms — drag-resizing
// a pane fires this at frame rate otherwise).

const RESIZE_DEBOUNCE_MS = 150;

export function XtermImpl() {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const [initialDims, setInitialDims] = useState<{ cols: number; rows: number } | null>(null);
  const [mountStatus, setMountStatus] = useState<"mounting" | "ready">("mounting");

  // Phase 7 step 1: stand up xterm. We don't open the PTY until xterm has
  // measured itself — the daemon needs the initial cols/rows.
  useEffect(() => {
    if (!hostRef.current) return;
    const term = new Terminal({
      convertEol: false, // PTY already sends CRLFs from a real terminal
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
      fontSize: 12,
      theme: { background: "#0f0f11", foreground: "#e5e7eb" },
      cursorBlink: true,
      allowProposedApi: true,
    });
    const fit = new FitAddon();
    const links = new WebLinksAddon();
    term.loadAddon(fit);
    term.loadAddon(links);
    term.open(hostRef.current);
    try {
      fit.fit();
    } catch {
      // Initial mount may have 0 dimensions for a frame. The ResizeObserver
      // below will re-fit once layout has settled.
    }
    termRef.current = term;
    fitRef.current = fit;

    setInitialDims({ cols: term.cols, rows: term.rows });
    setMountStatus("ready");

    return () => {
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, []);

  // Phase 7 step 2: open the PTY once we know the dimensions. usePty owns
  // the open/close lifecycle and pipes pty.output → onOutput.
  const pty = usePty({
    cols: initialDims?.cols ?? 80,
    rows: initialDims?.rows ?? 24,
    env: { TERM: "xterm-256color", COLORTERM: "truecolor" },
    onOutput: (bytes) => {
      termRef.current?.write(bytes);
    },
    onExit: ({ exitCode, signal }) => {
      const term = termRef.current;
      if (!term) return;
      const tail = signal ? `signal=${signal}` : `code=${exitCode}`;
      term.write(`\r\n\x1b[2m[process exited (${tail})]\x1b[0m\r\n`);
      term.options.disableStdin = true;
    },
  });

  // Stable ref to the latest pty state so the wire-up effects below can
  // run once and stay decoupled from pty's per-render identity churn.
  const ptyRef = useRef(pty);
  ptyRef.current = pty;

  // Phase 7 step 3: wire term.onData → pty.input. Gated on
  // status === "ready" inside the callback so keystrokes typed during
  // the bootstrap second don't fire requests before pty_id is known.
  useEffect(() => {
    const term = termRef.current;
    if (!term) return;
    const sub = term.onData((data) => {
      const current = ptyRef.current;
      if (current.status !== "ready") return;
      current.send(data);
    });
    return () => sub.dispose();
  }, []);

  // Phase 7 step 4: container ResizeObserver → fit.fit() → pty.resize.
  // Debounced so drag-resizing the pane edge doesn't fire 60×/sec.
  useEffect(() => {
    if (!hostRef.current) return;
    const host = hostRef.current;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const ro = new ResizeObserver(() => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => {
        const fit = fitRef.current;
        const term = termRef.current;
        if (!fit || !term) return;
        try {
          fit.fit();
        } catch {
          return;
        }
        ptyRef.current.resize(term.cols, term.rows);
      }, RESIZE_DEBOUNCE_MS);
    });
    ro.observe(host);
    return () => {
      ro.disconnect();
      if (timer) clearTimeout(timer);
    };
  }, []);

  const indicator = (() => {
    if (mountStatus === "mounting") return "mounting…";
    switch (pty.status) {
      case "opening":
        return "opening…";
      case "ready":
        return pty.droppedCount > 0 ? `ready (truncated ${pty.droppedCount})` : "ready";
      case "exited":
        return pty.signal ? `exited (signal ${pty.signal})` : `exited (code ${pty.exitCode ?? "?"})`;
      case "error":
        return `error: ${pty.error ?? "unknown"}`;
    }
  })();

  return (
    <div className="flex h-full w-full flex-col">
      <div
        className="flex items-center justify-between border-b border-zinc-800 bg-zinc-950 px-2 py-1 text-xs text-zinc-500"
        data-testid="terminal-status"
        data-state={pty.status}
      >
        <span>terminal</span>
        <span>{indicator}</span>
      </div>
      <div ref={hostRef} className="xterm-host min-h-0 flex-1" />
    </div>
  );
}
