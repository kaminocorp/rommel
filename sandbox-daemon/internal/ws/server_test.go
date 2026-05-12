package ws_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/config"
	fsx "github.com/rommel-ade/rommel/sandbox-daemon/internal/fs"
	wsx "github.com/rommel-ade/rommel/sandbox-daemon/internal/ws"
)

// --- test harness -----------------------------------------------------------

type harness struct {
	srv  *httptest.Server
	priv ed25519.PrivateKey
	cfg  *config.Config
	root string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}

	root := t.TempDir()

	cfg := &config.Config{
		Port:          0, // not used by httptest
		WorkspaceRoot: root,
		WID:           "ws-test",
		TokenPublic:   pub,
	}

	fsh := &fsx.Handler{Root: cfg.WorkspaceRoot}
	routes := map[string]wsx.Route{
		"system.ping": {Fn: func(_ context.Context, _ *protogen.SessionTokenClaims, _ json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
			return json.RawMessage(`{"ok":true}`), nil
		}},
		"fs.read": {
			RequiredScope: []protogen.SessionTokenClaimsScopeElem{
				protogen.SessionTokenClaimsScopeElemFsR,
				protogen.SessionTokenClaimsScopeElemFsRw,
			},
			Fn: fsh.Read,
		},
		"fs.write": {
			RequiredScope: []protogen.SessionTokenClaimsScopeElem{protogen.SessionTokenClaimsScopeElemFsRw},
			Fn:            fsh.NotImplemented("fs.write"),
		},
	}

	srv := wsx.NewServer(cfg, routes)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", srv.HandleHealth)
	mux.HandleFunc("/ws", srv.HandleWS)
	ts := httptest.NewServer(mux)

	t.Cleanup(func() { ts.Close() })

	return &harness{srv: ts, priv: priv, cfg: cfg, root: root}
}

// mintToken signs a valid JWT for this harness. Optional knobs let individual
// tests tweak just the field they care about (wid, scope, exp) without
// duplicating the claim bag.
type tokenOpts struct {
	wid    string
	scope  []string
	exp    time.Duration
	issuer string
	aud    string
}

func (h *harness) mintToken(t *testing.T, opts tokenOpts) string {
	t.Helper()
	if opts.wid == "" {
		opts.wid = h.cfg.WID
	}
	if opts.scope == nil {
		opts.scope = []string{"fs:rw", "pty:rw"}
	}
	if opts.exp == 0 {
		opts.exp = 5 * time.Minute
	}
	if opts.issuer == "" {
		opts.issuer = "rommel-backend"
	}
	if opts.aud == "" {
		opts.aud = "rommel-daemon"
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   opts.issuer,
		"sub":   "user-test",
		"aud":   opts.aud,
		"wid":   opts.wid,
		"scope": opts.scope,
		"exp":   now.Add(opts.exp).Unix(),
		"iat":   now.Unix(),
		"jti":   uuid.NewString(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	s, err := tok.SignedString(h.priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func (h *harness) dial(t *testing.T, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	u, _ := url.Parse(h.srv.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	q := u.Query()
	if token != "" {
		q.Set("token", token)
	}
	u.RawQuery = q.Encode()
	return websocket.DefaultDialer.Dial(u.String(), nil)
}

func (h *harness) roundTrip(t *testing.T, conn *websocket.Conn, kind, typ string, payload any) *wsx.Frame {
	t.Helper()
	id := uuid.NewString()
	raw, _ := json.Marshal(payload)
	frame := wsx.Frame{
		Kind:    protogen.EnvelopeKind(kind),
		Type:    typ,
		ID:      &id,
		Payload: raw,
	}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	var out wsx.Frame
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.ID == nil || *out.ID != id {
		t.Fatalf("id mismatch: got %v want %s", out.ID, id)
	}
	if out.Type != typ {
		t.Fatalf("type mismatch: got %q want %q", out.Type, typ)
	}
	return &out
}

// --- tests ------------------------------------------------------------------

func TestHealthz(t *testing.T) {
	h := newHarness(t)
	resp, err := http.Get(h.srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
}

func TestUpgrade_MissingToken_401(t *testing.T) {
	h := newHarness(t)
	_, resp, err := h.dial(t, "")
	if err == nil {
		t.Fatal("expected dial error")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %v want 401", resp)
	}
}

func TestUpgrade_BadSignature_401(t *testing.T) {
	h := newHarness(t)
	// Sign with a different key.
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "rommel-backend", "sub": "u", "aud": "rommel-daemon",
		"wid": h.cfg.WID, "scope": []string{"fs:r"},
		"exp": now.Add(time.Minute).Unix(), "iat": now.Unix(), "jti": uuid.NewString(),
	}
	s, _ := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(otherPriv)

	_, resp, err := h.dial(t, s)
	if err == nil {
		t.Fatal("expected dial error")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %v want 401", resp)
	}
}

func TestUpgrade_WrongWID_401(t *testing.T) {
	h := newHarness(t)
	tok := h.mintToken(t, tokenOpts{wid: "some-other-workspace"})
	_, resp, err := h.dial(t, tok)
	if err == nil {
		t.Fatal("expected dial error")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %v want 401", resp)
	}
}

func TestUpgrade_ExpiredToken_401(t *testing.T) {
	h := newHarness(t)
	tok := h.mintToken(t, tokenOpts{exp: -time.Minute})
	_, resp, err := h.dial(t, tok)
	if err == nil {
		t.Fatal("expected dial error")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %v want 401", resp)
	}
}

func TestPing_RoundTrip(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "system.ping", nil)
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q want %q (err=%+v)", resp.Kind, protogen.EnvelopeKindResponse, resp.Error)
	}
}

func TestUnknownType_Errors(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.banana", nil)
	if resp.Kind != protogen.EnvelopeKindError {
		t.Fatalf("kind: got %q want error", resp.Kind)
	}
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeUnknownType {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeUnknownType)
	}
}

func TestFsRead_HappyPath_Utf8(t *testing.T) {
	h := newHarness(t)
	content := "hello, rommel\n"
	path := filepath.Join(h.root, "greeting.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "greeting.txt"})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q want response (err=%+v)", resp.Kind, resp.Error)
	}
	var body protogen.FsReadResponse
	if err := json.Unmarshal(resp.Payload, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Contents != content {
		t.Fatalf("contents: got %q want %q", body.Contents, content)
	}
	if body.Size != len(content) {
		t.Fatalf("size: got %d want %d", body.Size, len(content))
	}
	if body.Encoding != protogen.FsReadResponseEncodingUtf8 {
		t.Fatalf("encoding: got %q want utf-8", body.Encoding)
	}
}

func TestFsRead_Base64Binary(t *testing.T) {
	h := newHarness(t)
	bin := []byte{0x00, 0xff, 0x01, 0x80, 0x7f}
	path := filepath.Join(h.root, "blob.bin")
	if err := os.WriteFile(path, bin, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "blob.bin", "encoding": "base64"})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q want response (err=%+v)", resp.Kind, resp.Error)
	}
	var body protogen.FsReadResponse
	if err := json.Unmarshal(resp.Payload, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Encoding != protogen.FsReadResponseEncodingBase64 {
		t.Fatalf("encoding: got %q want base64", body.Encoding)
	}
}

func TestFsRead_AbsolutePath_Rejected(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "/etc/passwd"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFsInvalidPath {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFsInvalidPath)
	}
}

func TestFsRead_DotDotEscape_Rejected(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "../../etc/passwd"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFsInvalidPath {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFsInvalidPath)
	}
}

func TestFsRead_NotFound(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "no-such-file"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFsNotFound {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFsNotFound)
	}
}

func TestFsWrite_StubReturnsNotImplemented(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.write", map[string]any{"path": "x", "contents": "y"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeNotImplemented {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeNotImplemented)
	}
}

func TestFsRead_InsufficientScope_Forbidden(t *testing.T) {
	h := newHarness(t)
	// Token has only pty:rw — no fs scope.
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"pty:rw"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "x"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeForbidden {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeForbidden)
	}
}

func TestBadEnvelope_BadRequest(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Skip the type field entirely.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"kind":"request"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	var out wsx.Frame
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.Error == nil || out.Error.Code != wsx.ErrCodeBadRequest {
		t.Fatalf("code: got %+v want %q", out.Error, wsx.ErrCodeBadRequest)
	}
}

// sanity: make sure protogen envelope enum string we encode actually matches.
func TestEnvelopeKindStrings(t *testing.T) {
	if string(protogen.EnvelopeKindRequest) != "request" {
		t.Fatalf("envelope kind drift: %q", protogen.EnvelopeKindRequest)
	}
	if !strings.HasPrefix(fmt.Sprint(protogen.EnvelopeKindError), "error") {
		t.Fatalf("envelope kind drift: %q", protogen.EnvelopeKindError)
	}
}
