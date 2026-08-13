# Remux

Run multiple coding agents in parallel using git worktrees and tmux.

## Why Remux?

When running multiple AI coding agents simultaneously, each needs its own isolated
environment to avoid conflicts. Remux automates this by:

- Creating git worktrees so each agent works on its own branch
- Managing tmux sessions for easy switching between agents
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
the tmux session of that workspace.

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
4. Run any `on_create` hooks from `.remux.yaml`
5. Open a tmux session in the new workspace

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

Opens a tmux session for an existing workspace.

### List workspaces

```bash
remux list
```

### Remove current workspace

```bash
remux drop
```

Removes the current worktree, unregisters it, and kills the tmux session. Fails if there are uncommitted changes.

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

Define tmux windows (tabs) that are automatically created when opening a workspace:

```yaml
tabs:
  - name: editor
    cmd: vim
  - name: server
    cmd: "npm start -- --port {{ space.Port }}"
  - name: shell
```

Each tab becomes a tmux window in the session, after the agent tab. Tab names and commands support the same template expressions as env vars and hooks.

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

### Hooks

- `on_create` - Runs when workspace is created (non-blocking)
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
