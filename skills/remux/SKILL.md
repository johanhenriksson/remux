---
name: remux
description: Use when working with remux, or delegating work to a background coding agent in its own git worktree. remux creates a worktree, port, env, hooks and an agent per workspace, in herdr or tmux. Covers `remux new|open|list|close|drop|next|prev`, delegating ("delegate this", "hand this off", "spin up a worktree/agent") and following up on delegated agents, `.remux.yaml` / `.remux.local.yaml` config (backend, env, env_file, hooks, tabs, agent), template variables like `{{ space.Port }}`, remux-shell, and the `remux daemon` WORKFLOW.md orchestrator. Use whenever a lead session at a repo root has Linear ticket work to do.
---

# remux

`remux` creates a git worktree per workspace, allocates it a unique port, renders
its env, runs lifecycle hooks, and starts a session with a coding agent in the
first tab. The session runs in **herdr** or **tmux** (the backend). remux owns the
worktree, port, env, hooks and teardown in both cases.

Install: `go install github.com/johanhenriksson/remux@latest` (binary lands in `$GOPATH/bin`).

## Delegating work to a background agent

1. **Pick names.**
   - **Branch**: from the ticket when there is one (Linear `gitBranchName`, e.g.
     `johanh/ait-2679-quick-search-based-company-enrichment`), otherwise short
     kebab-case derived from the work.
   - **Workspace name** (`--name`): the ticket id (`ait-2679`) or a short slug.
     It keeps the directory and `{{ space.ID }}`-based database names short.
   - The **agent name** defaults to the ticket id in the branch (`ait-2679`),
     otherwise a slug of the branch; override with `--agent-name`. It must be
     unique among live herdr agents.
   - If the branch already exists, pass `--reuse` (remux otherwise asks
     interactively, which blocks).

2. **Write the prompt to a file.** The delegated agent starts with an *empty*
   context window and sees only this prompt. Write it to the scratchpad directory
   as markdown:

   - **Goal**: one or two sentences.
   - **Context**: decisions made and why, files and symbols involved
     (`path/to/file.go:funcName`), measurements, constraints, anything rejected
     and why.
   - **Scope**: what to do, and explicitly what not to do.
   - **Verification**: the test/build command to run when done.
   - **Delivery**: commit, push, draft PR, ticket updates, per the repo's CLAUDE.md.

   Be generous; an under-specified prompt means the agent guesses. Never
   reference "what we discussed": the agent cannot see this conversation.

3. **Create the workspace**, from the repo root, based on the remote default branch:

   ```bash
   git fetch origin
   remux new -d --base origin/main --name <short-name> -p "$(cat <scratchpad>/<name>.md)" <branch>
   ```

   - Always pass `-d`: without it remux attaches to the session and blocks.
   - `remux new` blocks while the `on_create` hooks run (installs, databases),
     which can take minutes. Use a long timeout or run it in the background.
   - Add `--no-tabs` to skip the configured tabs (dev servers) when the agent
     doesn't need them; it can start servers itself on its own ports.
   - With the herdr backend, remux prints `started agent "<name>"`. If the agent
     is blocked at startup (e.g. a folder trust prompt), remux waits up to two
     minutes and then fails without sending the prompt. Show the user
     `herdr agent read <name>` and ask before answering it, then send the prompt
     with `herdr agent prompt <name> "$(cat <file>)"`.

4. **Report back**: branch, workspace and agent name, port (`remux list`), and how
   to check on it: `herdr agent list`, `herdr agent read <name>`.

### Follow-ups

Send follow-up work (review fixes, adjustments) to the same agent with
`herdr agent prompt <name> "..."`. If the workspace was closed, run
`remux open -d -p "..." <short-name>` to bring it back with a fresh agent. If only
the agent exited, start a new one in its pane (`herdr agent list` / `herdr pane
list` for the pane id): `herdr agent start <name> --kind claude --pane <pane-id>
-- --permission-mode auto`, then `herdr agent prompt`. Never do the work in the
repo root checkout.

### Cleaning up

When the work is merged or abandoned, and only after checking the worktree has
nothing uncommitted or unpushed (`git -C <path> status`, `git -C <path> log @{u}..`):

```bash
remux drop <short-name>
```

This closes the session (stopping tab processes), runs the `on_drop` hooks (e.g.
dropping the workspace database), removes the worktree and unregisters it. The
branch is kept. Plans or notes kept only in the worktree are lost; copy them out
first. Add `-f` only when you have confirmed uncommitted changes can go.

### Notes

- Uncommitted changes in the root checkout do **not** carry over: the worktree
  branches from `--base`. If the work depends on them, say so and let the user
  decide whether to commit.
- One branch per delegation, unless the work has genuinely separate parts.
- A lead session never checks out branches in the repo root checkout; other
  sessions share it. Ticket work always goes to a workspace, however small.

## Commands

All commands take `--path <dir>` to override the worktree root (default `~/.remux`).

| Command | Purpose |
|---|---|
| `remux new <branch>` | Create branch + worktree + registry entry, start the session, run `on_create`, open tabs |
| `remux open <name>` | Focus the session, or recreate it if it was closed |
| `remux list` | List workspaces for the current repo (name, branch, port, status, path) |
| `remux close [name]` | End the session, keep the worktree |
| `remux drop [name]` | End the session, run `on_drop`, remove the worktree, unregister |
| `remux next` / `remux prev` | Cycle between active remux sessions |
| `remux herdr setup` | Install `remux-shell` (see below) |
| `remux daemon [workflow-file]` | Run the orchestrator against `WORKFLOW.md` |
| `remux version` | Print version |

Flags on `new` (and where noted `open`):

- `-d/--detach`: don't attach (`new`, `open`). Always use it non-interactively.
- `-p/--prompt "<text>"`: initial prompt for the agent in tab 1, also the
  `{{ prompt }}` template variable (`new`, `open`).
- `--agent <cmd>`: agent for this invocation (`new`, `open`).
- `--agent-name <name>`: herdr agent name (`new`, `open`).
- `--no-tabs`: only the agent tab (`new`, `open`).
- `--base <ref>`: base for a new branch, e.g. `origin/main` (default: current HEAD).
- `--reuse`: reuse an existing branch without asking.
- `--name <name>`: workspace name instead of the branch name.
- `remux drop -f/--force`: drop despite uncommitted changes.

`open`/`close`/`drop` take the full workspace name (`<repo>-<name>`), the branch,
or the short name; `close`/`drop` without an argument act on the workspace
containing the cwd.

## Configuration

`.remux.yaml` in the repo root, optionally overlaid by `.remux.local.yaml`
(gitignore that one; it's for machine-local overrides). Merge rules: `env` maps
merge key-by-key, `agent`, `backend` and `env_file` override if non-empty, `tabs`
are replaced wholesale if the local file defines any, and each hook list is
replaced independently.

```yaml
backend: herdr            # or tmux (default); REMUX_BACKEND sets a user default
agent: codex
env_file: .env.local      # optional: also write the env here on create

env:
  PORT: "{{ space.Port }}"
  DATABASE_URL: "postgres://localhost/myapp_{{ space.ID }}"

hooks:
  on_create: [npm install]          # once, before tabs start; failures warn only
  on_open: [docker compose up -d]   # blocking; failure aborts open
  on_drop: [docker compose down]    # blocking; runs after the session is closed

tabs:
  - name: editor
    cmd: nvim .
  - name: server
    cmd: "npm start -- --port {{ space.Port }}"
  - name: shell
```

Tabs are created **after** the agent tab. Tab fields: `name`, `cmd`, `prompt`
(text typed into the tab after `cmd` starts), `await` (string to wait for before
sending the prompt), `timeout` (seconds, default 60).

### Env and backends

- **tmux**: env is set on the session, so every window sees it, including ones
  opened by hand.
- **herdr**: remux passes env to every tab it creates. Tabs and splits opened by
  hand only get it through `remux-shell`: run `remux herdr setup` and set the
  printed `default_shell` in `~/.config/herdr/config.toml`. remux-shell adds the
  env of the workspace containing the pane's directory and execs the login shell.
  Without it, use `env_file` and load the file (direnv `dotenv`, Task `dotenv:`).

### Template variables

Values in `env`, `hooks`, and tab fields support `{{ expr }}` interpolation,
evaluated with [expr-lang](https://expr-lang.org) (not Go templates; the daemon's
`WORKFLOW.md` body is the one place that uses Go templates):

| Variable | Meaning |
|---|---|
| `space.Name` | Workspace name |
| `space.Path` | Worktree path |
| `space.Port` | Allocated port (11010, 11020, …) |
| `space.ID` | Name with `-` and `/` replaced by `_`, safe for db/identifier names |
| `space.RepoRoot` | Repo root the workspace belongs to |
| `prompt` | The `-p` prompt |
| `env.*` | Host environment variables |

## Agent tab

Tab 1 always runs the agent (unless `agent: none`). Default is `claude`.
`claude` and `codex` are known agents and get sensible flags (claude runs in auto
permission mode). With tmux the `-p` prompt is appended as a positional argument;
with herdr the agent is started with `herdr agent start` and the prompt is
submitted with `herdr agent prompt`. Any other value is used verbatim as a
command line, so `--agent "claude --model opus"` works.

## Daemon

`remux daemon` reads front-matter from `WORKFLOW.md`: a `tracker` block (e.g.
Linear via MCP), a `workflow` block mapping step names to trigger/progress/next
statuses, `polling.interval_ms`, and `agent.command` / `agent.max_concurrent`.
The Markdown body is a Go template rendered per issue (`.Identifier`, `.Title`,
`.Description`, `.URL`, `.StepName`, `.BranchName`, `.BaseBranch`, `.Attempt`)
and passed to the agent as the final positional argument.

`agent.command` starting with `codex` runs through Codex exec JSONL mode (remux
adds `exec` and `--json` if missing); otherwise the claude stream-json invocation
is used. Add `--debug` for verbose logging.

## Gotchas

- `remux list` only shows workspaces whose repo root matches the current repo;
  run it outside a repo to see everything.
- tmux: inside tmux, `open`/`next`/`prev` switch client; outside, they attach.
- herdr: `open`, `next` and `prev` without `-d` focus the workspace in the herdr UI.
- herdr can't open panes if `default_shell` points at a missing `remux-shell`;
  remove the config line before uninstalling remux.
