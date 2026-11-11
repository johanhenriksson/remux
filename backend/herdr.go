package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/johanhenriksson/remux/config"
)

// Runner executes the herdr CLI with the given arguments and returns stdout.
type Runner func(args ...string) ([]byte, error)

// herdrBackend runs each workspace as a herdr worktree workspace. remux
// creates the git worktree itself; herdr attaches to it with `worktree open`,
// which groups it under the main repository's workspace.
//
// Workspaces are located by worktree path through `herdr worktree list`
// rather than by a stored workspace ID, since the user can close and reopen
// workspaces in the herdr UI at any time.
type herdrBackend struct {
	run   Runner
	sleep func(time.Duration)
	env   func(string) string
}

// NewHerdr returns the herdr backend. A nil runner calls the herdr binary.
func NewHerdr(run Runner) Backend {
	if run == nil {
		run = execHerdr
	}
	return &herdrBackend{run: run, sleep: time.Sleep, env: os.Getenv}
}

func execHerdr(args ...string) ([]byte, error) {
	cmd := exec.Command("herdr", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("herdr %s: %s", strings.Join(args[:min(len(args), 2)], " "), msg)
	}
	return out, nil
}

func (h *herdrBackend) Name() string { return Herdr }

// call runs herdr and decodes the "result" field of its JSON response.
func (h *herdrBackend) call(result any, args ...string) error {
	out, err := h.run(args...)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return fmt.Errorf("herdr %s: invalid response: %w", strings.Join(args[:min(len(args), 2)], " "), err)
	}
	return json.Unmarshal(resp.Result, result)
}

type herdrWorktree struct {
	Path            string `json:"path"`
	OpenWorkspaceID string `json:"open_workspace_id"`
}

// workspaceID returns the ID of the open herdr workspace for the space's
// worktree, or "" if none is open.
func (h *herdrBackend) workspaceID(space Space) (string, error) {
	var res struct {
		Worktrees []herdrWorktree `json:"worktrees"`
	}
	if err := h.call(&res, "worktree", "list", "--cwd", space.RepoRoot); err != nil {
		return "", err
	}
	want := normalizePath(space.Path)
	for _, wt := range res.Worktrees {
		if normalizePath(wt.Path) == want {
			return wt.OpenWorkspaceID, nil
		}
	}
	return "", nil
}

// normalizePath resolves symlinks so that e.g. /tmp and /private/tmp compare
// equal on macOS. herdr reports resolved paths.
func normalizePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

func (h *herdrBackend) Exists(space Space) (bool, error) {
	id, err := h.workspaceID(space)
	return id != "", err
}

// Create opens the worktree as a herdr workspace without focusing it.
//
// `worktree open` cannot pass env, and herdr env only reaches the process it
// launches, so the initial tab is replaced by one created with env.
func (h *herdrBackend) Create(space Space, env map[string]string) error {
	var opened struct {
		AlreadyOpen bool `json:"already_open"`
		Workspace   struct {
			ID string `json:"workspace_id"`
		} `json:"workspace"`
		Tab struct {
			ID string `json:"tab_id"`
		} `json:"tab"`
	}
	if err := h.call(&opened, "worktree", "open", "--cwd", space.RepoRoot, "--path", space.Path, "--no-focus"); err != nil {
		return err
	}
	if opened.AlreadyOpen || len(env) == 0 {
		return nil
	}

	if _, err := h.createTab(opened.Workspace.ID, space.Path, "shell", env); err != nil {
		return err
	}
	return h.call(nil, "tab", "close", opened.Tab.ID)
}

// createTab creates a tab with env and returns its root pane ID.
func (h *herdrBackend) createTab(workspaceID, cwd, label string, env map[string]string) (string, error) {
	args := []string{"tab", "create", "--workspace", workspaceID, "--cwd", cwd}
	if label != "" {
		args = append(args, "--label", label)
	}
	args = append(args, herdrEnvArgs(env)...)
	args = append(args, "--no-focus")

	var created struct {
		RootPane struct {
			ID string `json:"pane_id"`
		} `json:"root_pane"`
	}
	if err := h.call(&created, args...); err != nil {
		return "", err
	}
	return created.RootPane.ID, nil
}

func herdrEnvArgs(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]string, 0, 2*len(keys))
	for _, k := range keys {
		args = append(args, "--env", k+"="+env[k])
	}
	return args
}

func (h *herdrBackend) AddTabs(space Space, tabs []config.Tab, env map[string]string) error {
	if len(tabs) == 0 {
		return nil
	}
	ws, err := h.workspaceID(space)
	if err != nil {
		return err
	}
	if ws == "" {
		return fmt.Errorf("no herdr workspace open for %s", space.Path)
	}

	var panes struct {
		Panes []struct {
			ID    string `json:"pane_id"`
			TabID string `json:"tab_id"`
		} `json:"panes"`
	}
	if err := h.call(&panes, "pane", "list", "--workspace", ws); err != nil {
		return err
	}
	if len(panes.Panes) == 0 {
		return fmt.Errorf("herdr workspace %s has no panes", ws)
	}

	for i, tab := range tabs {
		var pane string
		if i == 0 {
			// First tab reuses the workspace's initial shell
			pane = panes.Panes[0].ID
			if tab.Name != "" {
				if err := h.call(nil, "tab", "rename", panes.Panes[0].TabID, tab.Name); err != nil {
					return err
				}
			}
		} else {
			pane, err = h.createTab(ws, space.Path, tab.Name, env)
			if err != nil {
				return err
			}
		}

		if tab.Cmd != "" {
			if err := h.call(nil, "pane", "run", pane, tab.Cmd); err != nil {
				return err
			}
		}

		prompt := strings.TrimSpace(tab.Prompt)
		if prompt == "" {
			continue
		}
		if tab.Await != "" {
			timeout := awaitTimeout(tab)
			fmt.Printf("waiting for %q in tab %q...\n", tab.Await, tab.Name)
			ms := strconv.FormatInt(timeout.Milliseconds(), 10)
			if err := h.call(nil, "pane", "wait-output", pane, "--match", tab.Await, "--timeout", ms); err != nil {
				return fmt.Errorf("tab %q: %w", tab.Name, err)
			}
		}
		// Give TUI time to become interactive after awaited text appears
		h.sleep(1 * time.Second)
		if err := h.call(nil, "pane", "send-text", pane, prompt); err != nil {
			return err
		}
		h.sleep(100 * time.Millisecond)
		if err := h.call(nil, "pane", "send-keys", pane, "enter"); err != nil {
			return err
		}
	}
	return nil
}

func (h *herdrBackend) Focus(space Space) error {
	ws, err := h.workspaceID(space)
	if err != nil {
		return err
	}
	if ws == "" {
		return fmt.Errorf("no herdr workspace open for %s", space.Path)
	}
	return h.call(nil, "workspace", "focus", ws)
}

// Close closes the herdr workspace. The checkout stays on disk and can be
// reopened with `worktree open`.
func (h *herdrBackend) Close(space Space) error {
	ws, err := h.workspaceID(space)
	if err != nil || ws == "" {
		return err
	}
	return h.call(nil, "workspace", "close", ws)
}

func (h *herdrBackend) Current(candidates []Space) (*Space, bool, error) {
	if h.env("HERDR_ENV") != "1" {
		return nil, false, nil
	}
	current := h.env("HERDR_WORKSPACE_ID")
	if current == "" {
		return nil, true, nil
	}
	for i := range candidates {
		ws, err := h.workspaceID(candidates[i])
		if err != nil {
			return nil, true, err
		}
		if ws == current {
			return &candidates[i], true, nil
		}
	}
	return nil, true, nil
}
