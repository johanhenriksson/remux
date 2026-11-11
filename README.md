# Remux

Run multiple coding agents in parallel using git worktrees and tmux (or [herdr](#backends)).

## Why Remux?

When running multiple AI coding agents simultaneously, each needs its own isolated
environment to avoid conflicts. Remux automates this by:

- Creating git worktrees so each agent works on its own branch
- Managing tmux sessions (or herdr workspaces) for easy switching between agents
- Allocating unique ports so web apps can run in parallel
- Running setup hooks automatically (npm install, docker compose, etc.)

## Example: Parallel Web Development

Run your main branch and a feature branch side by side:

```bash
# In your repo, create a workspace for a new feature
remux new add-auth
```

Each workspace gets a unique port (11010, 11020, etc.)
Configure your app to use it in `.remux.yaml`:

```yaml
env:
  PORT: "{{ space.Port }}"
  DATABASE_URL: "postgres://localhost/myapp_{{ space.ID }}"

hooks:
  on_create:
    - npm install
  on_open:
    - docker compose up -d
  on_drop:
    - docker compose down
```

The environment variables defined in `.remux.yaml` will be available in
the session of that workspace.

Now your main branch runs on port 11010 and add-auth runs on 11020.

## Installation

```bash
go install github.com/johanhenriksson/remux@latest
```

This installs the `remux` command to your `$GOPATH/bin` directory. Make sure it's in your `PATH`.

## Usage

### Create a new workspace

```bash
remux new feature-branch
```

This will:
1. Create a new Git branch `feature-branch`
2. Create a worktree in `~/.remux/repo-feature-branch`
3. Register the workspace and allocate a port
4. Start a session for the workspace (tmux or herdr, see [Backends](#backends))
5. Write the `env_file` if configured, and run any `on_create` hooks from `.remux.yaml`
6. Open the configured tabs and attach to the session (`-d` to stay detached)

Use `--dest` to specify a different destination directory:

```bash
remux new feature-branch --dest ~/workspaces
```

Use `-p` to give the workspace an initial prompt:

```bash
remux new mything -p "create mything"
```

The prompt is passed to the agent running in the first tab (`claude 'create
mything'`) so it starts working immediately. It also fills the `{{ prompt }}`
template variable, so a configured tab with `prompt: "{{ prompt }}"` receives it
too. Works with `remux open` as well.

Pick the agent with `--agent`:

```bash
remux new mything --agent codex -p "create mything"
```

### Open an existing workspace

```bash
remux open feature-branch
```

Opens the session for an existing workspace, recreating it if it was closed.
Use `-d` to recreate it without attaching.

### Close a workspace session

```bash
remux close [name]
```

Ends the session but keeps the worktree, so `remux open` can bring it back.

### List workspaces

```bash
remux list
```

### Remove current workspace

```bash
remux drop
```

Closes the session, runs `on_drop` hooks, then removes the worktree and unregisters it. Fails if there are uncommitted changes.

## Configuration

Create a `.remux.yaml` file in your repository root to configure workspace behavior:

```yaml
env:
  APP_PORT: "{{ space.Port }}"
  WORKSPACE: "{{ space.Name }}"

hooks:
  on_create:
    - npm install
  on_open:
    - echo "Opening {{ space.Name }}"
  on_drop:
    - docker compose down
```

### Template expressions

Configuration values support template expressions using `{{ }}` syntax:

| Variable | Description |
|----------|-------------|
| `space.Name` | Workspace name |
| `space.Path` | Full worktree path |
| `space.Port` | Allocated port number |
| `space.ID` | Sanitized name (hyphens replaced with underscores) |
| `space.RepoRoot` | Associated repository root |
| `env.*` | Environment variables |

### Tabs

Define tabs that are automatically created when opening a workspace:

```yaml
tabs:
  - name: editor
    cmd: vim
  - name: server
    cmd: "npm start -- --port {{ space.Port }}"
  - name: shell
```

Each tab becomes a tmux window (or herdr tab) in the session, after the agent tab. Tab names and commands support the same template expressions as env vars and hooks.

### Agent

The first tab always runs the agent. `claude` is used by default; override it per
workspace in config, or per invocation with `--agent`:

```yaml
agent: codex
```

Known agents (`claude`, `codex`) are started with sensible flags — `claude` runs
in auto permission mode — and receive the `-p` prompt as a positional argument.
Any other value is used as the command line verbatim, so
`--agent "claude --model opus"` works too. Use `none` to skip the agent tab.

### Env file

```yaml
env_file: .env.local
```

When set, remux writes the resolved `env` (plus `SPACE_PORT`) as a dotenv file at
this path, relative to the worktree, when the workspace is created. Shells and
tools that don't inherit the session env (a herdr tab opened by hand, an editor,
Task's `dotenv:`) can load it. Add the file to `.gitignore`.

### Backends

The session backend is chosen with `backend`:

```yaml
backend: herdr   # or tmux (default)
```

Like other keys, it can be set in `.remux.yaml` or overridden per machine in
`.remux.local.yaml`. To make herdr your default across repositories, set
`REMUX_BACKEND=herdr` in your shell; config files take precedence over it.

remux always owns the worktree, port, env, hooks and teardown. The backend only
runs the interactive session:

- **tmux** (default): one session per workspace, named after it. Session env
  reaches every window, including ones you open by hand.
- **herdr**: remux creates the worktree, then attaches it with
  `herdr worktree open`, so it shows up grouped under the repository's
  workspace with its branch. Each tab is a herdr tab. herdr env only reaches the
  process it launches, so remux passes env to every tab it creates, but tabs you
  open by hand won't have it; use `env_file` for those. `remux close` closes the
  herdr workspace and keeps the checkout. `remux next`/`prev` cycle through remux
  workspaces in herdr when run inside herdr. herdr will also open a workspace for
  the main checkout if none is open.

### Hooks

- `on_create` - Runs once, when the first session for a new workspace is created (non-blocking, before tabs start)
- `on_open` - Runs when workspace is opened (blocking)
- `on_drop` - Runs when workspace is removed (blocking)

### Daemon agent commands

The daemon reads `agent.command` from `WORKFLOW.md`. Claude commands keep the
existing stream-json invocation, while commands starting with `codex` run through
Codex exec JSONL mode:

```yaml
agent:
  command: "codex exec --json"
```

Remux adds `exec` and `--json` when they are omitted, then appends the rendered
workflow prompt as the final positional argument. Add any Codex model, profile,
sandbox, or approval flags directly to `agent.command`.

## License

MIT
