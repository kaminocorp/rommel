"use client";

import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";

// V1 terminal: pure-DOM xterm, no PTY wiring. Phase 6 wires pty.open /
// pty.input / pty.output / pty.resize via DaemonConnection.rpc /
// .subscribe.

export function XtermImpl() {
  const hostRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!hostRef.current) return;
    const term = new Terminal({
      convertEol: true,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
      fontSize: 12,
      theme: { background: "#0f0f11", foreground: "#e5e7eb" },
      cursorBlink: true,
      disableStdin: true,
    });
    const fit = new FitAddon();
    const links = new WebLinksAddon();
    term.loadAddon(fit);
    term.loadAddon(links);
    term.open(hostRef.current);
    fit.fit();
    term.writeln("Welcome to Rommel.");
    term.writeln("PTY wiring lands in Phase 6 (pty.open / pty.input / pty.output).");
    term.writeln("");

    const ro = new ResizeObserver(() => {
      try {
        fit.fit();
      } catch {
        // ignore: container may be 0-sized during initial measurement
      }
    });
    ro.observe(hostRef.current);

    return () => {
      ro.disconnect();
      term.dispose();
    };
  }, []);

  return <div ref={hostRef} className="h-full w-full" />;
}
