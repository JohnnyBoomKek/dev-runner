# dev-runner

[![CI](https://github.com/JohnnyBoomKek/dev-runner/actions/workflows/ci.yml/badge.svg)](https://github.com/JohnnyBoomKek/dev-runner/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/JohnnyBoomKek/dev-runner)](https://github.com/JohnnyBoomKek/dev-runner/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A worktree-aware TUI for running local development projects. Start services, switch between Git worktrees, run one-off actions, inspect assigned ports, and follow logs without rebuilding the same shell workflow in every terminal.

```text
╭ Projects ─────────────────────────────────────────────────────────────╮
│ shop 2/4     api 1/3                                                 │
╰───────────────────────────────────────────────────────────────────────╯
╭ Worktrees · active first ─────────╮ ╭ Services & actions ────────────╮
│ ● feature/checkout         2/3    │ │ web                 running    │
│ ● main                     1/3    │ │ worker              stopped    │
│ ── inactive · 2 ──                │ │ migrate [m]         action     │
│ ○ feature/search           0/3    │ │ test [t]            action     │
╰───────────────────────────────────╯ ╰─────────────────────────────────╯
╭ Runtime ──────────────────────────╮ ╭ Logs · web ─────────────────────╮
│ PORT=8001                         │ │ GET /health 200                 │
│ REDIS_PORT=6381                   │ │ server ready on 127.0.0.1:8001 │
╰───────────────────────────────────╯ ╰─────────────────────────────────╯
```

dev-runner is macOS-first and also ships Linux binaries. It is designed for local use: project plugins are trusted shell commands executed with your user permissions.

## Why

- One plugin per project; every Git worktree inherits it automatically.
- Running worktrees sort first, while inactive worktrees remain one keypress away.
- Services survive closing the TUI because supervision is handled by tmux.
- Preferred ports are assigned automatically and remain globally unique across worktrees.
- Docker Compose project names, runtime data, logs, and ports are isolated per worktree.
- Actions turn routines such as `migrate`, `test`, and `build-css` into visible controls.

## Requirements

At runtime:

- Git
- tmux
- Docker with Compose only when a plugin declares a Compose service

Go 1.23 or newer is needed only when installing from source.

```bash
brew install tmux       # macOS
sudo apt install tmux   # Debian/Ubuntu
```

## Install

### Prebuilt binary

Download the archive matching your system from the [latest release](https://github.com/JohnnyBoomKek/dev-runner/releases/latest). For example:

```bash
# macOS Apple Silicon
curl -LO https://github.com/JohnnyBoomKek/dev-runner/releases/latest/download/dev-runner-darwin-arm64.tar.gz
tar -xzf dev-runner-darwin-arm64.tar.gz
sudo install dev-runner /usr/local/bin/dev-runner
dev-runner -version
```

Available archives:

| Platform | Archive |
| --- | --- |
| macOS Apple Silicon | `dev-runner-darwin-arm64.tar.gz` |
| macOS Intel | `dev-runner-darwin-amd64.tar.gz` |
| Linux x86_64 | `dev-runner-linux-amd64.tar.gz` |
| Linux ARM64 | `dev-runner-linux-arm64.tar.gz` |

### From source

```bash
go install github.com/JohnnyBoomKek/dev-runner@latest
```

## Quick start

Run the TUI:

```bash
dev-runner
```

By default it scans `~/Projects` and `~/Work`. Press `n` to add another discovery root and create a plugin, or add `.runner/config.yaml` to a project manually:

```yaml
name: shop

services:
  - name: web
    command: ./scripts/dev-server --port "${PORT}"
    ready_command: curl --fail --silent "http://127.0.0.1:${PORT}/health"
    ports:
      PORT: 8000

  - name: worker
    command: ./scripts/worker
    depends_on: [web]

actions:
  - name: migrate
    key: m
    command: ./scripts/migrate
```

Restart dev-runner or press `R`. The project appears once, with all existing worktrees beneath it. Commands run from the selected worktree.

## Keyboard

| Key | Action |
| --- | --- |
| `h` / `l` | Move between projects, worktrees, and services |
| `j` / `k`, `g` / `G` | Move selection or jump to first/last |
| `enter` or `s` | Start a service or run an action |
| `x` | Stop selected service |
| `r` | Restart selected service |
| `S` / `X` | Start/stop all services in the selected worktree |
| `i` | Open the runtime and environment inspector |
| `:` | Open a shell in the selected worktree |
| `n` | Add a root or create a project plugin |
| `R` | Rescan projects and worktrees |
| `q` | Close the TUI; supervised services keep running |

## Plugin reference

dev-runner checks the project's main checkout for a plugin in this order:

1. `local/*/runner.yaml`
2. `.runner/config.yaml`
3. `.runner.yaml`

A service defines exactly one of `command` or `compose`.

```yaml
name: example
runtime_dir: .runner

services:
  - name: redis
    compose:
      file: compose.yaml
      services: [redis]
    ports:
      REDIS_PORT: 6379

  - name: web
    command: ./scripts/web
    stop_command: ./scripts/web-stop
    ready_command: curl --fail --silent "http://127.0.0.1:${PORT}/health"
    depends_on: [redis]
    ports:
      PORT: 8000
    env:
      REDIS_URL: redis://127.0.0.1:${REDIS_PORT}/0

actions:
  - name: migrate
    key: m
    command: ./scripts/migrate
    env:
      DJANGO_SETTINGS_MODULE: app.settings.local
```

### Runtime environment

Declared port names become environment variables shared by all services and actions in that worktree. These built-ins are also available:

| Variable | Value |
| --- | --- |
| `DEV_RUNNER_CONTEXT_ID` | Stable worktree runtime identifier |
| `DEV_RUNNER_PROJECT` | Project name |
| `DEV_RUNNER_ROOT` | Main project checkout |
| `DEV_RUNNER_WORKTREE` | Selected worktree path |
| `DEV_RUNNER_DATA_DIR` | Worktree-local runtime data directory |
| `DEV_RUNNER_URL` | `http://127.0.0.1:$PORT` when `PORT` exists |

Sensitive environment values are masked in the inspector and CLI output. They are still passed to configured commands.

## CLI

The same binary supports exact, scriptable operations:

```bash
dev-runner list [--all]
dev-runner start <project/worktree|context-id> [service]
dev-runner stop <project/worktree|context-id> [service]
dev-runner status <project/worktree|context-id> [service]
dev-runner env <project/worktree|context-id>
dev-runner run <project/worktree|context-id> <action>
```

Use `dev-runner list` to copy an unambiguous selector or context ID.

## State and cleanup

Port assignments, roots, status, and logs are stored in the user config directory:

- macOS: `~/Library/Application Support/dev-runner`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/dev-runner`

`stop` preserves Docker Compose volumes and worktree data. There is currently no destructive clean command.

## Development

```bash
go test -race ./...
go vet ./...
go run .
```

Tagged releases are built by GitHub Actions for both macOS architectures and Linux amd64/arm64.

## License

[MIT](LICENSE)
