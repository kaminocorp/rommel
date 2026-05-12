// Package workspace implements the workspace.* primitives. Today only
// workspace.info — workspace.health and workspace.shutdown will follow.
package workspace

import (
	"encoding/json"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
)

// DaemonVersion is overridden via -ldflags at build time; defaults to a
// development sentinel so test output stays diff-stable.
var DaemonVersion = "0.0.0-dev"

type InfoHandler struct {
	WID string
}

// Info returns a workspace.info payload. The Repo field is intentionally
// nil until git plumbing lands in a later phase — the schema marks it
// optional ("Cloned repo info (omitted if no repo imported)").
func (h *InfoHandler) Info() (json.RawMessage, error) {
	info := protogen.WorkspaceInfo{
		ID:            h.WID,
		DaemonVersion: DaemonVersion,
	}
	return json.Marshal(info)
}
