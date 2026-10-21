package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The worktree panel lists and removes the task worktrees cut from each
// team's hub. It only works where the API can see the hub (local / single
// host); elsewhere the hubs are reported unavailable.

type worktreeInfo struct {
	Path      string `json:"path"`
	Branch    string `json:"branch,omitempty"`
	Head      string `json:"head,omitempty"`
	Project   string `json:"project"`
	Hub       string `json:"hub"`
	IssueKey  string `json:"issue_key,omitempty"`
	IssueID   string `json:"issue_id,omitempty"`
	Title     string `json:"title,omitempty"`
	Status    string `json:"status,omitempty"`
	Running   bool   `json:"running"`
	Dirty     int    `json:"dirty"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
	Files     int    `json:"files"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Locked    bool   `json:"locked"`
	Prunable  bool   `json:"prunable"`
}

type hubInfo struct {
	Project   string `json:"project"`
	Hub       string `json:"hub"`
	Main      string `json:"main"`
	Available bool   `json:"available"`
}

type wtEntry struct {
	path, head, branch  string
	bare, locked, prune bool
}

func parseWorktreeList(out string) []wtEntry {
	var res []wtEntry
	var cur *wtEntry
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			res = append(res, wtEntry{path: strings.TrimPrefix(line, "worktree ")})
			cur = &res[len(res)-1]
		case cur == nil:
		case strings.HasPrefix(line, "HEAD "):
			cur.head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "bare":
			cur.bare = true
		case strings.HasPrefix(line, "locked"):
			cur.locked = true
		case strings.HasPrefix(line, "prunable"):
			cur.prune = true
		}
	}
	return res
}

var shortstatRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

func parseShortstat(s string) (files, add, del int) {
	m := shortstatRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, 0
	}
	files, _ = strconv.Atoi(m[1])
	add, _ = strconv.Atoi(m[2])
	del, _ = strconv.Atoi(m[3])
	return
}

func (s *Service) workspaceHubs(ws string) []hubInfo {
	var out []hubInfo
	for _, t := range s.snapshotTeams() {
		if t.WorkspaceID != ws || t.Spec == nil || t.Spec.Repo.Hub == "" {
			continue
		}
		h := hubInfo{Project: t.ProjectID, Hub: t.Spec.Repo.Hub, Main: t.Spec.Repo.Main}
		if h.Main == "" {
			h.Main = "main"
		}
		if st, err := os.Stat(filepath.Join(h.Hub, ".git")); err == nil && st != nil {
			h.Available = true
		}
		out = append(out, h)
	}
	return out
}

func (s *Service) listWorktrees(ctx context.Context, r *http.Request) ([]hubInfo, []worktreeInfo) {
	hubs := s.workspaceHubs(wsID(r))
	wts := []worktreeInfo{}
	for _, h := range hubs {
		if !h.Available {
			continue
		}
		res := run(ctx, h.Hub, "git", "worktree", "list", "--porcelain")
		if res.code != 0 {
			continue
		}
		t := s.Team(h.Project)
		for _, e := range parseWorktreeList(res.out) {
			if e.bare || filepath.Clean(e.path) == filepath.Clean(h.Hub) {
				continue
			}
			wi := worktreeInfo{Path: e.path, Branch: e.branch, Head: e.head, Project: h.Project, Hub: h.Hub, Locked: e.locked, Prunable: e.prune}
			if t != nil && e.branch != "" && strings.HasPrefix(e.branch, t.Spec.Repo.BranchPrefix) {
				wi.IssueKey = strings.ToUpper(strings.TrimPrefix(e.branch, t.Spec.Repo.BranchPrefix))
			}
			if !e.prune {
				if st := run(ctx, e.path, "git", "status", "--porcelain"); st.code == 0 {
					for _, l := range strings.Split(strings.TrimSpace(st.out), "\n") {
						if l != "" {
							wi.Dirty++
						}
					}
				}
				if lr := run(ctx, e.path, "git", "rev-list", "--left-right", "--count", h.Main+"...HEAD"); lr.code == 0 {
					f := strings.Fields(lr.out)
					if len(f) == 2 {
						wi.Behind, _ = strconv.Atoi(f[0])
						wi.Ahead, _ = strconv.Atoi(f[1])
					}
				}
				if ds := run(ctx, e.path, "git", "diff", "--shortstat", h.Main+"...HEAD"); ds.code == 0 {
					wi.Files, wi.Additions, wi.Deletions = parseShortstat(ds.out)
				}
			}
			if wi.IssueKey != "" {
				if v, err := s.viewByKey(r, wi.IssueKey); err == nil && v != nil {
					wi.IssueID, wi.Title, wi.Status, wi.Running = v.ID, v.Title, v.Status, v.Running != nil
				}
			}
			wts = append(wts, wi)
		}
	}
	return hubs, wts
}

// hWorktrees: GET /worktrees[?issue=KEY]
func (s *Service) hWorktrees(w http.ResponseWriter, r *http.Request) {
	hubs, wts := s.listWorktrees(r.Context(), r)
	if key := strings.ToUpper(r.URL.Query().Get("issue")); key != "" {
		f := wts[:0]
		for _, x := range wts {
			if x.IssueKey == key {
				f = append(f, x)
			}
		}
		wts = f
	}
	if hubs == nil {
		hubs = []hubInfo{}
	}
	writeJSON(w, 200, map[string]any{"hubs": hubs, "worktrees": wts})
}

// hWorktreeRemove: POST /worktrees/remove {path, force} — only paths the list
// reports, never the hub, never while the issue's run is live.
func (s *Service) hWorktreeRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path  string `json:"path"`
		Force bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		writeErr(w, 400, "bad_request", "path is required")
		return
	}
	_, wts := s.listWorktrees(r.Context(), r)
	var hit *worktreeInfo
	for i := range wts {
		if filepath.Clean(wts[i].Path) == filepath.Clean(req.Path) {
			hit = &wts[i]
		}
	}
	if hit == nil {
		writeErr(w, 404, "not_found", "not a worktree of this workspace's hubs")
		return
	}
	if hit.Running {
		writeErr(w, 409, "running", "the issue's agent run is using this worktree")
		return
	}
	if hit.Dirty > 0 && !req.Force {
		writeErr(w, 409, "dirty", strconv.Itoa(hit.Dirty)+" uncommitted change(s); remove with force")
		return
	}
	args := []string{"worktree", "remove"}
	if req.Force {
		args = append(args, "--force")
	}
	if hit.Prunable {
		args = []string{"worktree", "prune"}
	} else {
		args = append(args, hit.Path)
	}
	if res := run(r.Context(), hit.Hub, "git", args...); res.code != 0 {
		writeErr(w, 409, "git", strings.TrimSpace(res.err))
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "path": hit.Path})
}
