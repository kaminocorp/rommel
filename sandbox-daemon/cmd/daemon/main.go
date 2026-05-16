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
	"path/filepath"
	"syscall"
	"time"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/config"
	fsx "github.com/rommel-ade/rommel/sandbox-daemon/internal/fs"
	funnelx "github.com/rommel-ade/rommel/sandbox-daemon/internal/funnel"
	gitx "github.com/rommel-ade/rommel/sandbox-daemon/internal/git"
	ptyx "github.com/rommel-ade/rommel/sandbox-daemon/internal/pty"
	wsx "github.com/rommel-ade/rommel/sandbox-daemon/internal/ws"
	wsinfo "github.com/rommel-ade/rommel/sandbox-daemon/internal/workspace"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("daemon: %v", err)
	}

	ptyh := ptyx.New(cfg.WorkspaceRoot)
	fsh := &fsx.Handler{Root: cfg.WorkspaceRoot}
	srv := wsx.NewServer(cfg, buildRoutes(cfg, ptyh, fsh)).
		WithLifecycle(ptyh).
		WithLifecycle(fsh) // Phase 1: fs.watch needs OnDisconnect cleanup

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

	// Release the fs.watch fsnotify.Watcher + its event-loop goroutine.
	// HTTP-level shutdown drains in-flight connections; this cleans up the
	// process-wide resources those connections may have allocated.
	fsh.Stop()
}

// buildRoutes assembles the primitive → handler map. Required scopes mirror
// the session-token.json enum: fs:r grants read-only fs.*; fs:rw grants both;
// pty:rw is the only pty scope; funnel:r/rw and git:r/rw follow the same pattern.
func buildRoutes(cfg *config.Config, ptyh *ptyx.Handler, fsh *fsx.Handler) map[string]wsx.Route {
	funh := &funnelx.Handler{Root: filepath.Join(cfg.WorkspaceRoot, "rommel")}
	gith := &gitx.Handler{Root: cfg.WorkspaceRoot}
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
	funnelR := []protogen.SessionTokenClaimsScopeElem{
		protogen.SessionTokenClaimsScopeElemFunnelR,
		protogen.SessionTokenClaimsScopeElemFunnelRw,
	}
	funnelRw := []protogen.SessionTokenClaimsScopeElem{
		protogen.SessionTokenClaimsScopeElemFunnelRw,
	}

	gitR := []protogen.SessionTokenClaimsScopeElem{
		protogen.SessionTokenClaimsScopeElemGitR,
		protogen.SessionTokenClaimsScopeElemGitRw,
	}
	gitRw := []protogen.SessionTokenClaimsScopeElem{
		protogen.SessionTokenClaimsScopeElemGitRw,
	}

	return map[string]wsx.Route{
		// system.* — daemon-level. No scope required: a valid token implies
		// the right to ping the daemon you connected to.
		"system.ping": {Fn: pingHandler},

		// workspace.* — metadata about the workspace. Read-equivalent; no
		// scope required for v1 (token already says you're authorised here).
		"workspace.info": {Fn: func(_ wsx.HandlerCtx, _ json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
			b, err := info.Info()
			if err != nil {
				return nil, &protogen.EnvelopeError{Code: wsx.ErrCodeInternal, Message: err.Error()}
			}
			return b, nil
		}},

		// fs.* — sandboxed under workspace root. Phase 1 added real fs.watch
		// (streaming via Publisher + ConnLifecycle cleanup). Phase 4 completed
		// the write side with mkdir / move / delete.
		"fs.read":   {RequiredScope: fsR, Fn: fsh.Read},
		"fs.list":   {RequiredScope: fsR, Fn: fsh.List},
		"fs.write":  {RequiredScope: fsRw, Fn: fsh.Write},
		"fs.watch":  {RequiredScope: fsR, Fn: fsh.Watch},
		"fs.mkdir":  {RequiredScope: fsRw, Fn: fsh.Mkdir},
		"fs.move":   {RequiredScope: fsRw, Fn: fsh.Move},
		"fs.delete": {RequiredScope: fsRw, Fn: fsh.Delete},

		// funnel.* — sandboxed under <WorkspaceRoot>/rommel/.
		"funnel.list":    {RequiredScope: funnelR, Fn: funh.List},
		"funnel.read":    {RequiredScope: funnelR, Fn: funh.Read},
		"funnel.promote": {RequiredScope: funnelRw, Fn: funh.Promote},

		// pty.* — real as of Phase 7.
		"pty.open":        {RequiredScope: ptyRw, Fn: ptyh.Open},
		"pty.input":       {RequiredScope: ptyRw, Fn: ptyh.Input},
		"pty.resize":      {RequiredScope: ptyRw, Fn: ptyh.Resize},
		"pty.close":       {RequiredScope: ptyRw, Fn: ptyh.Close},
		"pty.start_agent": {RequiredScope: ptyRw, Fn: ptyh.StartAgent}, // Phase 3

		// git.* — Phase 2 structured git primitives.
		"git.status":       {RequiredScope: gitR, Fn: gith.Status},
		"git.diff":         {RequiredScope: gitR, Fn: gith.Diff},
		"git.branch.list":   {RequiredScope: gitR, Fn: gith.BranchList},
		"git.branch.create": {RequiredScope: gitRw, Fn: gith.BranchCreate},
		"git.branch.switch": {RequiredScope: gitRw, Fn: gith.BranchSwitch},
		"git.commit":        {RequiredScope: gitRw, Fn: gith.Commit},
	}
}

func pingHandler(_ wsx.HandlerCtx, _ json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	body, _ := json.Marshal(map[string]any{
		"ok": true,
		"ts": time.Now().UTC().Format(time.RFC3339Nano),
	})
	return body, nil
}
