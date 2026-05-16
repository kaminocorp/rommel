// Package git implements structured git primitives (git.status, and later
// git.diff, git.commit, git.branch.*, etc.).
//
// Implementation strategy (per next-steps.md): shell out to the `git` CLI
// (already present in the workspace image) and parse porcelain output.
// This keeps the dependency footprint tiny (no go-git) while still giving the
// frontend clean typed data instead of forcing every consumer to parse
// `git status --porcelain` in JavaScript.
//
// All commands are executed with the workspace root as working directory and
// are therefore automatically sandboxed to the current workspace.
package git

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/ws"
)

type Handler struct {
	Root string // absolute workspace root
}

// runGit executes a git command. If workdir is empty it uses the workspace root.
func (h *Handler) runGit(workdir string, args ...string) ([]byte, error) {
	if workdir == "" {
		workdir = h.Root
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = workdir
	return cmd.Output()
}

// Status implements git.status.
// It shells out to `git status --porcelain=v1 -b --ahead-behind` and returns
// a structured response suitable for a StatusBar pill and future Git panels.
func (h *Handler) Status(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.GitStatusRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, errBody(ws.ErrCodeBadRequest, "git.status: invalid payload: "+err.Error())
		}
	}

	// Determine the directory to run git in.
	// If a sub-path is given we still run from Root but can scope later if needed.
	workdir := h.Root
	if req.Path != "" {
		workdir = filepath.Join(h.Root, req.Path)
	}

	out, err := h.runGit(workdir, "status", "--porcelain=v1", "-b", "--ahead-behind", "--untracked=normal")
	if err != nil {
		// Common case: not a git repository at all.
		if exitErr, ok := err.(*exec.ExitError); ok && bytes.Contains(exitErr.Stderr, []byte("not a git repository")) {
			resp := protogen.GitStatusResponse{
				Branch: "",
				Clean:  true,
			}
			b, _ := json.Marshal(resp)
			return b, nil
		}
		return nil, errBody(ws.ErrCodeGitFailed, "git.status: "+err.Error())
	}

	status, parseErr := parsePorcelainV1(string(out))
	if parseErr != nil {
		return nil, errBody(ws.ErrCodeInternal, "git.status: parse error: "+parseErr.Error())
	}

	b, err := json.Marshal(status)
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, "git.status: marshal: "+err.Error())
	}
	return b, nil
}

// parsePorcelainV1 parses the output of `git status --porcelain=v1 -b --ahead-behind`.
func parsePorcelainV1(raw string) (*protogen.GitStatusResponse, error) {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	if len(lines) == 0 {
		return &protogen.GitStatusResponse{Clean: true}, nil
	}

	resp := &protogen.GitStatusResponse{
		Changes: []protogen.GitFileChange{},
	}

	first := lines[0]
	// Branch line: "## main...origin/main [ahead 3, behind 1]" or "## HEAD (no branch)"
	if strings.HasPrefix(first, "## ") {
		branchLine := strings.TrimPrefix(first, "## ")
		parts := strings.Fields(branchLine)

		if len(parts) > 0 {
			branch := parts[0]
			if idx := strings.Index(branch, "..."); idx != -1 {
				branch = branch[:idx]
			}
			if branch == "HEAD" || branch == "(HEAD" {
				resp.Branch = "HEAD"
			} else {
				resp.Branch = branch
			}
		}

		// Parse ahead/behind from the branch line, e.g. "[ahead 3, behind 2]"
		for _, p := range parts {
			if strings.Contains(p, "ahead") || strings.Contains(p, "behind") {
				a, b := parseAheadBehind(p)
				resp.Ahead = a
				resp.Behind = b
			}
		}
	}

	// File change lines
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		change := parsePorcelainLine(line)
		if change != nil {
			resp.Changes = append(resp.Changes, *change)
		}
	}

	resp.Clean = len(resp.Changes) == 0
	return resp, nil
}

func parseAheadBehind(token string) (ahead, behind int) {
	token = strings.Trim(token, "[]")
	parts := strings.Split(token, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "ahead ") {
			if n, err := strconv.Atoi(strings.TrimPrefix(p, "ahead ")); err == nil {
				ahead = n
			}
		}
		if strings.HasPrefix(p, "behind ") {
			if n, err := strconv.Atoi(strings.TrimPrefix(p, "behind ")); err == nil {
				behind = n
			}
		}
	}
	return ahead, behind
}

func parsePorcelainLine(line string) *protogen.GitFileChange {
	if len(line) < 3 {
		return nil
	}

	xy := line[:2]
	pathPart := strings.TrimSpace(line[3:])

	var change protogen.GitFileChange
	change.Path = pathPart

	// Handle rename/copy syntax: "R  old -> new"
	if strings.Contains(pathPart, " -> ") {
		parts := strings.Split(pathPart, " -> ")
		if len(parts) == 2 {
			change.OldPath = strings.TrimSpace(parts[0])
			change.Path = strings.TrimSpace(parts[1])
			change.Status = protogen.GitFileChangeStatusRenamed
		}
	}

	switch {
	case xy[0] == 'M' || xy[1] == 'M':
		change.Status = "modified"
		change.Staged = xy[0] == 'M'
	case xy[0] == 'A':
		change.Status = "added"
		change.Staged = true
	case xy[0] == 'D' || xy[1] == 'D':
		change.Status = "deleted"
		change.Staged = xy[0] == 'D'
	case xy[0] == 'R':
		change.Status = "renamed"
	case xy[0] == 'C':
		change.Status = "copied"
	case xy[0] == '?' && xy[1] == '?':
		change.Status = "untracked"
	case xy[0] == '!' && xy[1] == '!':
		change.Status = "ignored"
	case xy[0] == 'U' || xy[1] == 'U':
		change.Status = "conflicted"
	default:
		change.Status = "modified"
	}

	return &change
}

func errBody(code, msg string) *protogen.EnvelopeError {
	return &protogen.EnvelopeError{Code: code, Message: msg}
}

// Diff implements git.diff.
func (h *Handler) Diff(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.GitDiffRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, errBody(ws.ErrCodeBadRequest, "git.diff: invalid payload: "+err.Error())
		}
	}

	args := []string{"diff", "--no-color", "--unified=3"}
	if req.Staged {
		args = append(args, "--cached")
	}
	if req.Path != "" {
		args = append(args, req.Path)
	}

	out, err := h.runGit("", args...)
	if err != nil {
		// It's okay to have no diff (exit code 1 from git diff when there are changes is normal in some cases)
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			out = exitErr.Stdout
		} else {
			return nil, errBody(ws.ErrCodeGitFailed, "git.diff: "+err.Error())
		}
	}

	resp := protogen.GitDiffResponse{
		Diff: string(out),
		Path: req.Path,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, "git.diff: marshal: "+err.Error())
	}
	return b, nil
}

// BranchList implements git.branch.list
func (h *Handler) BranchList(_ ws.HandlerCtx, _ json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	out, err := h.runGit("", "branch", "--list", "--format=%(refname:short)")
	if err != nil {
		return nil, errBody(ws.ErrCodeGitFailed, "git.branch.list: "+err.Error())
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	branches := []string{}
	current := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "* ") {
			current = strings.TrimPrefix(line, "* ")
			branches = append(branches, current)
		} else {
			branches = append(branches, line)
		}
	}

	if current == "" && len(branches) > 0 {
		current = branches[0]
	}

	resp := protogen.GitBranchListResponse{
		Current:  current,
		Branches: branches,
	}

	b, _ := json.Marshal(resp)
	return b, nil
}

// BranchCreate implements git.branch.create
func (h *Handler) BranchCreate(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.GitBranchCreateRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "git.branch.create: invalid payload")
	}
	if req.Name == "" {
		return nil, errBody(ws.ErrCodeBadRequest, "git.branch.create: branch name required")
	}

	args := []string{"branch", req.Name}
	_, err := h.runGit("", args...)
	if err != nil {
		return nil, errBody(ws.ErrCodeGitFailed, "git.branch.create: "+err.Error())
	}

	if req.Checkout {
		_, err = h.runGit("", "checkout", req.Name)
		if err != nil {
			return nil, errBody(ws.ErrCodeGitFailed, "git.branch.create: checkout failed: "+err.Error())
		}
	}

	resp := protogen.GitBranchCreateResponse{Name: req.Name}
	b, _ := json.Marshal(resp)
	return b, nil
}

// BranchSwitch implements git.branch.switch
func (h *Handler) BranchSwitch(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.GitBranchSwitchRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "git.branch.switch: invalid payload")
	}

	_, err := h.runGit("", "checkout", req.Name)
	if err != nil {
		return nil, errBody(ws.ErrCodeGitFailed, "git.branch.switch: "+err.Error())
	}

	resp := protogen.GitBranchSwitchResponse{Name: req.Name}
	b, _ := json.Marshal(resp)
	return b, nil
}

// Commit implements git.commit (basic version).
func (h *Handler) Commit(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.GitCommitRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "git.commit: invalid payload")
	}
	if req.Message == "" {
		return nil, errBody(ws.ErrCodeBadRequest, "git.commit: message is required")
	}

	// Stage specific files if provided
	if len(req.Files) > 0 {
		args := append([]string{"add", "--"}, req.Files...)
		if _, err := h.runGit("", args...); err != nil {
			return nil, errBody(ws.ErrCodeGitFailed, "git.commit: git add failed: "+err.Error())
		}
	}

	// Commit. `git commit` writes "nothing to commit" to stdout and exits 1
	// when there's nothing staged — preserve that as a dedicated error path.
	if out, err := h.runGit("", "commit", "-m", req.Message); err != nil {
		if bytes.Contains(out, []byte("nothing to commit")) {
			return nil, errBody(ws.ErrCodeGitFailed, "git.commit: nothing to commit")
		}
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return nil, errBody(ws.ErrCodeGitFailed, "git.commit: "+strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, errBody(ws.ErrCodeGitFailed, "git.commit: "+err.Error())
	}

	// Resolve the OID of the commit we just made. `git rev-parse HEAD` is the
	// canonical way; the previous '[' '\]' string-search of commit output was
	// fragile across git versions and locales.
	oid := ""
	if out, err := h.runGit("", "rev-parse", "HEAD"); err == nil {
		oid = strings.TrimSpace(string(out))
	}

	resp := protogen.GitCommitResponse{
		Oid:     oid,
		Message: req.Message,
	}

	b, _ := json.Marshal(resp)
	return b, nil
}
