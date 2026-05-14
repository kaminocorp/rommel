// DaemonConnection — the WebSocket wrapper. Plan §0.5, §step-4.
//
// Responsibilities:
//   - Open / close lifecycle (state machine: connecting → ready → reconnecting → failed | closed)
//   - Envelope encode/decode against @rommel/proto's Envelope union
//   - Request/response correlation by `id` (crypto.randomUUID)
//   - Exponential-backoff reconnect (250ms → 5s, give up after 5 attempts)
//   - Token-expiry refresh hook: caller passes a `refresh()` fn; the wrapper
//     invokes it on close-1008 / invalid_token / wall-clock approach.
//   - Reject in-flight rpc()s on close so React state never leaks (risk 4.9)
//
// Does NOT speak HTTP. Caller is responsible for first calling
// POST /workspaces/:id/sessions to get { daemon_url, token, expires_at }.

import type { Envelope } from "@rommel/proto";

export type ConnectionStatus =
  | "idle"
  | "connecting"
  | "ready"
  | "reconnecting"
  | "failed"
  | "closed";

export class DaemonClosedError extends Error {
  constructor(reason: string) {
    super(`daemon connection closed: ${reason}`);
    this.name = "DaemonClosedError";
  }
}

export class DaemonProtocolError extends Error {
  constructor(
    msg: string,
    readonly code?: string,
  ) {
    super(msg);
    this.name = "DaemonProtocolError";
  }
}

type Inflight = {
  resolve: (v: unknown) => void;
  reject: (e: unknown) => void;
  type: string;
};

type SubscribeHandler = (payload: unknown) => void;

export type DaemonConnectionOpts = {
  url: string;
  token: string;
  expiresAt?: Date | undefined;
  onStatusChange?: ((s: ConnectionStatus) => void) | undefined;
  // Caller-provided session refresh — typically `useCreateSession(workspaceId).mutateAsync`.
  // Returning a fresh `{ url, token, expiresAt }` re-opens the socket transparently.
  refresh?: (() => Promise<{ url: string; token: string; expiresAt?: Date }>) | undefined;
  // WebSocket constructor injection — tests pass a fake; default uses globalThis.
  webSocketImpl?: typeof WebSocket | undefined;
};

const INITIAL_BACKOFF_MS = 250;
const MAX_BACKOFF_MS = 5_000;
const MAX_RECONNECT_ATTEMPTS = 5;
const EXPIRY_REFRESH_WINDOW_MS = 30_000;
// notify() registers an inflight slot so daemon-side errors can be matched
// back to the originating call; the slot is purged this long after send so
// the map doesn't grow without bound across a long session.
const NOTIFY_INFLIGHT_TTL_MS = 5_000;

export class DaemonConnection {
  private socket: WebSocket | null = null;
  private status: ConnectionStatus = "idle";
  private inflight = new Map<string, Inflight>();
  private subscribers = new Map<string, Set<SubscribeHandler>>();
  private url: string;
  private token: string;
  private expiresAt: Date | undefined;
  private reconnectAttempt = 0;
  private closed = false;
  private readyResolvers: Array<() => void> = [];
  private readonly WS: typeof WebSocket;

  constructor(private readonly opts: DaemonConnectionOpts) {
    this.url = opts.url;
    this.token = opts.token;
    this.expiresAt = opts.expiresAt;
    this.WS = opts.webSocketImpl ?? (globalThis.WebSocket as typeof WebSocket);
  }

  getStatus(): ConnectionStatus {
    return this.status;
  }

  // Connect once. Resolves when the socket reaches `ready`. Subsequent
  // reconnects happen transparently — the returned promise only covers the
  // initial open.
  async connect(): Promise<void> {
    if (this.status !== "idle" && this.status !== "closed") {
      throw new Error(`connect() called from status=${this.status}`);
    }
    this.closed = false;
    this.setStatus("connecting");
    return new Promise<void>((resolve, reject) => {
      const onOpen = () => resolve();
      const onFail = (e: Error) => reject(e);
      this.readyResolvers.push(onOpen);
      try {
        this.openSocket(onFail);
      } catch (e) {
        reject(e instanceof Error ? e : new Error(String(e)));
      }
    });
  }

  // RPC: send a `request` envelope, await the matching `response` (or
  // `error`). Rejects with DaemonProtocolError on the `error` kind and with
  // DaemonClosedError if the socket dies before a response arrives.
  rpc<TReq, TRes>(type: string, payload: TReq): Promise<TRes> {
    return new Promise<TRes>((resolve, reject) => {
      if (this.closed) {
        reject(new DaemonClosedError("already closed"));
        return;
      }
      const id = crypto.randomUUID();
      this.inflight.set(id, {
        resolve: resolve as (v: unknown) => void,
        reject,
        type,
      });
      const frame: Envelope = {
        kind: "request",
        type,
        id,
        payload: payload as Record<string, unknown>,
      };
      this.send(frame);
    });
  }

  // Notify: fire-and-forget request. The daemon writes a response on error
  // (correlated by id) but not on success. Used by pty.input where the
  // ergonomics of `void` matter (every keystroke would otherwise mint an
  // unawaited Promise). We still mint an id so daemon-side errors can be
  // surfaced — the inflight slot is GC'd after a short window.
  notify<TReq>(type: string, payload: TReq): void {
    if (this.closed) return;
    const id = crypto.randomUUID();
    this.inflight.set(id, {
      resolve: () => {
        /* never called: success is silent on the wire */
      },
      reject: (e) => {
        const msg = e instanceof Error ? e.message : String(e);
        // Surface as console diagnostic — callers can attach their own
        // listener via subscribe() to a `pty.error` event if they want
        // structured handling.
        console.warn(`daemon notify(${type}) error: ${msg}`);
      },
      type,
    });
    setTimeout(() => {
      this.inflight.delete(id);
    }, NOTIFY_INFLIGHT_TTL_MS);
    const frame: Envelope = {
      kind: "request",
      type,
      id,
      payload: payload as Record<string, unknown>,
    };
    this.send(frame);
  }

  // Subscribe to server-pushed events of a given `type` (e.g. "pty.output",
  // "fs.watch"). Returns an unsubscribe fn — caller stores it in a React
  // useEffect cleanup so handlers don't fire against unmounted components
  // (risk 4.10).
  subscribe(type: string, handler: SubscribeHandler): () => void {
    let set = this.subscribers.get(type);
    if (!set) {
      set = new Set();
      this.subscribers.set(type, set);
    }
    set.add(handler);
    return () => {
      const s = this.subscribers.get(type);
      if (!s) return;
      s.delete(handler);
      if (s.size === 0) this.subscribers.delete(type);
    };
  }

  close(): void {
    this.closed = true;
    this.setStatus("closed");
    this.rejectInflight(new DaemonClosedError("closed by client"));
    try {
      this.socket?.close(1000, "client close");
    } catch {
      // ignore
    }
    this.socket = null;
  }

  // ---- internals --------------------------------------------------------

  private setStatus(next: ConnectionStatus): void {
    if (this.status === next) return;
    this.status = next;
    this.opts.onStatusChange?.(next);
  }

  private send(frame: Envelope): void {
    if (!this.socket || this.socket.readyState !== this.WS.OPEN) {
      // Surface as a protocol error so the caller can decide whether to
      // queue / retry. The reconnect path handles transient drops; this
      // branch is the "tried to send before connected" footgun.
      const id = frame.id;
      if (id) {
        this.inflight.get(id)?.reject(new DaemonClosedError("socket not open"));
        this.inflight.delete(id);
      }
      return;
    }
    this.socket.send(JSON.stringify(frame));
  }

  private openSocket(onInitialFail?: (e: Error) => void): void {
    const fullUrl = appendTokenIfMissing(this.url, this.token);
    let ws: WebSocket;
    try {
      ws = new this.WS(fullUrl);
    } catch (e) {
      const err = e instanceof Error ? e : new Error(String(e));
      onInitialFail?.(err);
      this.scheduleReconnect();
      return;
    }
    this.socket = ws;

    ws.onopen = () => {
      this.reconnectAttempt = 0;
      this.setStatus("ready");
      const resolvers = this.readyResolvers.splice(0);
      for (const r of resolvers) r();
      // If a refresh is needed soon, kick off a refresh timer.
      this.scheduleExpiryRefresh();
    };

    ws.onmessage = (ev) => {
      let frame: Envelope;
      try {
        frame = JSON.parse(typeof ev.data === "string" ? ev.data : String(ev.data));
      } catch {
        // Bad frame: ignore (logging is the caller's concern in v1).
        return;
      }
      this.dispatch(frame);
    };

    ws.onerror = () => {
      // Browsers don't surface error details; rely on the subsequent close
      // event for the status transition.
    };

    ws.onclose = (ev) => {
      const wasReady = this.status === "ready";
      this.socket = null;
      if (this.closed) return;
      // 1008 (policy) and 4401 (custom) signal auth-related closes — try
      // refresh first.
      if (ev.code === 1008 || ev.code === 4401) {
        void this.refreshAndReopen();
        return;
      }
      if (wasReady) this.setStatus("reconnecting");
      this.scheduleReconnect();
    };
  }

  private dispatch(frame: Envelope): void {
    if (frame.kind === "event") {
      const subs = this.subscribers.get(frame.type);
      if (!subs) return;
      for (const h of subs) h(frame.payload);
      return;
    }
    if (frame.id) {
      const pending = this.inflight.get(frame.id);
      if (!pending) return;
      this.inflight.delete(frame.id);
      if (frame.kind === "error") {
        const code = frame.error?.code ?? "unknown";
        const msg = frame.error?.message ?? "daemon error";
        // Surface invalid-token errors as a refresh opportunity.
        if (code === "invalid_token") void this.refreshAndReopen();
        pending.reject(new DaemonProtocolError(`${msg} (${code})`, code));
        return;
      }
      pending.resolve(frame.payload);
    }
  }

  private rejectInflight(err: Error): void {
    for (const [, p] of this.inflight) p.reject(err);
    this.inflight.clear();
  }

  private scheduleReconnect(): void {
    if (this.closed) return;
    if (this.reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
      this.setStatus("failed");
      this.rejectInflight(new DaemonClosedError("reconnect attempts exhausted"));
      return;
    }
    const backoff = Math.min(
      INITIAL_BACKOFF_MS * 2 ** this.reconnectAttempt,
      MAX_BACKOFF_MS,
    );
    this.reconnectAttempt += 1;
    this.setStatus("reconnecting");
    setTimeout(() => {
      if (this.closed) return;
      this.openSocket();
    }, backoff);
  }

  private scheduleExpiryRefresh(): void {
    if (!this.expiresAt || !this.opts.refresh) return;
    const ms = this.expiresAt.getTime() - Date.now() - EXPIRY_REFRESH_WINDOW_MS;
    if (ms <= 0) {
      void this.refreshAndReopen();
      return;
    }
    setTimeout(() => {
      if (this.closed) return;
      void this.refreshAndReopen();
    }, ms);
  }

  private async refreshAndReopen(): Promise<void> {
    if (!this.opts.refresh || this.closed) return;
    try {
      this.setStatus("reconnecting");
      const next = await this.opts.refresh();
      this.url = next.url;
      this.token = next.token;
      this.expiresAt = next.expiresAt;
      try {
        this.socket?.close(1000, "refresh");
      } catch {
        // ignore
      }
      this.socket = null;
      this.reconnectAttempt = 0;
      this.openSocket();
    } catch (e) {
      this.setStatus("failed");
      this.rejectInflight(
        new DaemonClosedError(`refresh failed: ${e instanceof Error ? e.message : String(e)}`),
      );
    }
  }
}

// Daemon `url` may already include `?token=...`; if not, append it.
function appendTokenIfMissing(url: string, token: string): string {
  if (/[?&]token=/.test(url)) return url;
  const sep = url.includes("?") ? "&" : "?";
  return `${url}${sep}token=${encodeURIComponent(token)}`;
}
