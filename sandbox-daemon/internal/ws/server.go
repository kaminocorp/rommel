package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/rommel-ade/rommel/sandbox-daemon/internal/auth"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/config"
	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
)

// Publisher is the seam handlers use to emit server-pushed events (e.g.
// pty.output, pty.exit, fs.watch-event). The pump implementation behind it
// applies drop-oldest backpressure — see internal/ws/pump.go. Payload must
// already be JSON-marshalled; the publisher wraps it in an event envelope.
//
// Returns false if the frame was dropped (either right now because the buffer
// was full, or eventually because the connection terminated). Handlers can
// surface drops to the client — the PTY handler emits pty.output_dropped.
type Publisher interface {
	Publish(eventType string, payload []byte) bool
}

// HandlerCtx is the per-request context passed to every HandlerFunc. The
// signature was bare context.Context through Phase 6; Phase 7 promotes it to
// a struct so streaming primitives can emit events (Publisher) and so
// connection-scoped state (ConnID — tagged on resources the handler owns)
// can be cleaned up at disconnect time.
type HandlerCtx struct {
	Ctx       context.Context
	Claims    *protogen.SessionTokenClaims
	Publisher Publisher
	ConnID    string
}

// HandlerFunc is the contract for one primitive. It receives the request
// payload as raw JSON (handlers unmarshal into their own typed shape) and
// returns either a response payload (marshalled to JSON) or an error envelope.
//
// Returning (nil, nil) is a fire-and-forget signal — no response frame is
// written. Used by pty.input (errors still come back via the error envelope
// correlated by request id; success is silent).
type HandlerFunc func(hc HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError)

// Route binds a primitive name to its handler plus its capability requirement.
// RequiredScope is any-of: if non-empty, the token must carry at least one
// listed scope.
type Route struct {
	RequiredScope []protogen.SessionTokenClaimsScopeElem
	Fn            HandlerFunc
}

// ConnLifecycle is implemented by handlers that own per-connection resources
// (currently just the PTY handler — PTYs are tagged with the connection that
// opened them and SIGTERMed when that connection drops). Handlers that
// implement this are passed into Server via WithLifecycle so runConn can call
// OnDisconnect during its deferred cleanup.
type ConnLifecycle interface {
	OnDisconnect(connID string)
}

type Server struct {
	cfg        *config.Config
	routes     map[string]Route
	lifecycles []ConnLifecycle
	up         websocket.Upgrader
}

func NewServer(cfg *config.Config, routes map[string]Route) *Server {
	return &Server{
		cfg:    cfg,
		routes: routes,
		up: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// v1 scaffolding: same-origin not required; the browser hits a
			// per-workspace URL behind Fly's internal DNS, not a public domain.
			// Tighten once the frontend's origin is known.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

// WithLifecycle registers a handler-side disconnect callback. Returns the
// server so the call can chain in main.go.
func (s *Server) WithLifecycle(l ConnLifecycle) *Server {
	s.lifecycles = append(s.lifecycles, l)
	return s
}

// HandleWS upgrades the connection at /ws?token=<jwt> and runs the per-conn loop.
func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	claims, err := auth.Verify(tok, s.cfg.TokenPublic, s.cfg.WID)
	if err != nil {
		log.Printf("ws: token rejected: %v", err)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	conn, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote a response header; nothing to do.
		log.Printf("ws: upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	s.runConn(r.Context(), conn, claims)
}

// HandleHealth answers GET /healthz with a static 200 — unauthenticated, used
// by Fly Machine probes and local smoke tests.
func (s *Server) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) runConn(ctx context.Context, conn *websocket.Conn, claims *protogen.SessionTokenClaims) {
	connID := uuid.NewString()

	// dropCount survives the lifetime of the pump; bumped from the dropFn
	// closure when frames go on the floor. We log a single warning so an
	// operator can see saturation without spamming a line per frame.
	var dropMu sync.Mutex
	var dropped int
	pump := startPump(conn, func(_ *Frame) {
		dropMu.Lock()
		dropped++
		dropMu.Unlock()
	})
	defer func() {
		dropMu.Lock()
		d := dropped
		dropMu.Unlock()
		if d > 0 {
			log.Printf("ws: conn %s dropped %d frames before close", connID, d)
		}
		pump.close()
		for _, l := range s.lifecycles {
			l.OnDisconnect(connID)
		}
	}()

	publisher := &connPublisher{pump: pump}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if !errors.Is(err, websocket.ErrCloseSent) {
				if ce, ok := err.(*websocket.CloseError); !ok || (ce.Code != websocket.CloseNormalClosure && ce.Code != websocket.CloseGoingAway) {
					log.Printf("ws: read error: %v", err)
				}
			}
			return
		}

		var frame Frame
		if err := json.Unmarshal(data, &frame); err != nil {
			pump.Send(errorFrame(&Frame{}, ErrCodeBadRequest, "invalid envelope JSON: "+err.Error()))
			continue
		}
		if frame.Type == "" {
			pump.Send(errorFrame(&frame, ErrCodeBadRequest, "envelope.type is required"))
			continue
		}
		if frame.Kind != protogen.EnvelopeKindRequest {
			pump.Send(errorFrame(&frame, ErrCodeBadRequest, "only kind=request is accepted from the client today"))
			continue
		}

		hc := HandlerCtx{
			Ctx:       ctx,
			Claims:    claims,
			Publisher: publisher,
			ConnID:    connID,
		}
		out := s.dispatch(hc, &frame)
		if out == nil {
			continue // fire-and-forget (e.g. pty.input)
		}
		pump.Send(out)
	}
}

func (s *Server) dispatch(hc HandlerCtx, req *Frame) *Frame {
	route, ok := s.routes[req.Type]
	if !ok {
		return errorFrame(req, ErrCodeUnknownType, "unknown primitive: "+req.Type)
	}
	if !auth.HasAnyScope(hc.Claims, route.RequiredScope...) {
		return errorFrame(req, ErrCodeForbidden, "token lacks required scope for "+req.Type)
	}

	payload, errBody := route.Fn(hc, req.Payload)
	if errBody != nil {
		return &Frame{
			Kind:  protogen.EnvelopeKindError,
			Type:  req.Type,
			ID:    req.ID,
			Error: errBody,
		}
	}
	if payload == nil {
		return nil
	}
	return response(req, payload)
}
