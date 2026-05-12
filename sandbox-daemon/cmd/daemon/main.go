// Command daemon is the rommel sandbox-daemon entrypoint. It boots an HTTP
// server with two routes:
//
//	GET /healthz     unauthenticated, returns "ok"
//	GET /ws?token=…  WebSocket upgrade, EdDSA-token-authenticated
//
// All routes outside those two return 404. The daemon shuts down gracefully
// on SIGINT/SIGTERM (~5s drain), suitable for Fly's stop signal handling.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/config"
	fsx "github.com/rommel-ade/rommel/sandbox-daemon/internal/fs"
	ptyx "github.com/rommel-ade/rommel/sandbox-daemon/internal/pty"
	wsx "github.com/rommel-ade/rommel/sandbox-daemon/internal/ws"
	wsinfo "github.com/rommel-ade/rommel/sandbox-daemon/internal/workspace"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("daemon: %v", err)
	}

	srv := wsx.NewServer(cfg, buildRoutes(cfg))

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", srv.HandleHealth)
	mux.HandleFunc("/ws", srv.HandleWS)

	addr := fmt.Sprintf(":%d", cfg.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("daemon: listening on %s (wid=%s, root=%s)", addr, cfg.WID, cfg.WorkspaceRoot)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("daemon: listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("daemon: shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("daemon: shutdown: %v", err)
		os.Exit(1)
	}
}

// buildRoutes assembles the primitive → handler map. Required scopes mirror
// the session-token.json enum: fs:r grants read-only fs.*; fs:rw grants both;
// pty:rw is the only pty scope (read/write is inherent to a PTY).
func buildRoutes(cfg *config.Config) map[string]wsx.Route {
	fsh := &fsx.Handler{Root: cfg.WorkspaceRoot}
	ptyh := &ptyx.Handler{}
	info := &wsinfo.InfoHandler{WID: cfg.WID}

	fsR := []protogen.SessionTokenClaimsScopeElem{
		protogen.SessionTokenClaimsScopeElemFsR,
		protogen.SessionTokenClaimsScopeElemFsRw,
	}
	fsRw := []protogen.SessionTokenClaimsScopeElem{
		protogen.SessionTokenClaimsScopeElemFsRw,
	}
	ptyRw := []protogen.SessionTokenClaimsScopeElem{
		protogen.SessionTokenClaimsScopeElemPtyRw,
	}

	return map[string]wsx.Route{
		// system.* — daemon-level. No scope required: a valid token implies
		// the right to ping the daemon you connected to.
		"system.ping": {Fn: pingHandler},

		// workspace.* — metadata about the workspace. Read-equivalent; no
		// scope required for v1 (token already says you're authorised here).
		"workspace.info": {Fn: func(_ context.Context, _ *protogen.SessionTokenClaims, _ json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
			b, err := info.Info()
			if err != nil {
				return nil, &protogen.EnvelopeError{Code: wsx.ErrCodeInternal, Message: err.Error()}
			}
			return b, nil
		}},

		// fs.* — sandboxed under workspace root. fs.read is real; the rest stub.
		"fs.read":  {RequiredScope: fsR, Fn: fsh.Read},
		"fs.write": {RequiredScope: fsRw, Fn: fsh.NotImplemented("fs.write")},
		"fs.list":  {RequiredScope: fsR, Fn: fsh.NotImplemented("fs.list")},
		"fs.watch": {RequiredScope: fsR, Fn: fsh.NotImplemented("fs.watch")},

		// pty.* — all stubbed in scaffolding.
		"pty.open":   {RequiredScope: ptyRw, Fn: ptyh.NotImplemented("pty.open")},
		"pty.input":  {RequiredScope: ptyRw, Fn: ptyh.NotImplemented("pty.input")},
		"pty.resize": {RequiredScope: ptyRw, Fn: ptyh.NotImplemented("pty.resize")},
	}
}

func pingHandler(_ context.Context, _ *protogen.SessionTokenClaims, _ json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	body, _ := json.Marshal(map[string]any{
		"ok": true,
		"ts": time.Now().UTC().Format(time.RFC3339Nano),
	})
	return body, nil
}
