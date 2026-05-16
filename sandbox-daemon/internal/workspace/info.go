// Package workspace implements the workspace.* primitives. Today only
// workspace.info — workspace.health and workspace.shutdown will follow.
package workspace

import (
	"encoding/json"
	"os/exec"
	"strings"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
)

// DaemonVersion is overridden via -ldflags at build time; defaults to a
// development sentinel so test output stays diff-stable.
var DaemonVersion = "0.0.0-dev"

type InfoHandler struct {
	WID string
}

// Info returns a workspace.info payload. With Phase 2 git primitives, we now
// populate the optional `repo` field when the workspace contains a git repository.
func (h *InfoHandler) Info() (json.RawMessage, error) {
	info := protogen.WorkspaceInfo{
		ID:            h.WID,
		DaemonVersion: DaemonVersion,
	}

	// Best-effort: try to populate repo info
	if repo, err := getRepoInfo(); err == nil {
		info.Repo = repo
	}

	return json.Marshal(info)
}

// getRepoInfo runs lightweight git commands to fill the repo object.
func getRepoInfo() (*protogen.WorkspaceInfoRepo, error) {
	// Get remote URL
	urlOut, err := exec.Command("git", "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return nil, err
	}
	url := strings.TrimSpace(string(urlOut))
	if url == "" {
		return nil, nil
	}

	// Current branch
	branchOut, _ := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	branch := strings.TrimSpace(string(branchOut))
	if branch == "HEAD" {
		branch = ""
	}

	// HEAD sha
	shaOut, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	headSha := strings.TrimSpace(string(shaOut))

	return &protogen.WorkspaceInfoRepo{
		Url:     url,
		Branch:  branch,
		HeadSha: headSha,
	}, nil
}
