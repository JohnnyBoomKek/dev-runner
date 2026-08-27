# dev-runner

A macOS-first, worktree-aware development TUI inspired by lazydocker. It keeps long-running services, Docker Compose dependencies, one-shot actions, logs, and an ad-hoc shell in one terminal interface.

## Requirements

- Go 1.23 or newer
- tmux 3.4 or newer
- Docker Compose when a plugin declares Compose services

## Install

Download the latest archive for your platform from the [GitHub Releases](https://github.com/johnnyboomkek/dev-runner/releases) page, unpack it, and put the `dev-runner` binary somewhere on your `PATH`.

Release artifacts are provided for:

- macOS Apple Silicon (`darwin-arm64`)
- macOS Intel (`darwin-amd64`)
- Linux x86_64 (`linux-amd64`)
- Linux ARM64 (`linux-arm64`)

The first run requires `tmux`:

```bash
brew install tmux       # macOS
sudo apt install tmux   # Debian/Ubuntu
```

## Run

```bash
go run .
```

By default, dev-runner scans both `~/Projects` and `~/Work` when they exist. Add another directory or a direct Git repository from the `n` wizard, or pass repeatable command-line roots such as `dev-runner -root ~/Code -root ~/Clients`.

The TUI is intentionally local and personal: it discovers Git worktrees, hides projects without a plugin, and keeps supervised services running in tmux after the interface closes.

The same binary has a scriptable interface for automation and exact operations:

```bash
dev-runner list
dev-runner start Lyrics2Learn/main
dev-runner status Lyrics2Learn/main
dev-runner env Lyrics2Learn/main
dev-runner run Lyrics2Learn/main migrate
dev-runner stop Lyrics2Learn/main
```

## Plugin format

The runner loads one plugin from the Git project's main checkout: `local/*/runner.yaml`, `.runner/config.yaml`, or `.runner.yaml`. Git worktrees are discovered automatically and every worktree inherits that project plugin. Runtime identity, ports, logs, Compose projects, and data remain isolated per worktree.

```yaml
name: Example
runtime_dir: local/example

services:
  - name: redis
    compose:
      file: local/example/compose.yaml
      services: [redis]
    ports:
      REDIS_PORT: 6380

  - name: web
    command: ./local/example/services/web
    depends_on: [redis]
    ready_command: curl --silent --output /dev/null http://127.0.0.1:${PORT}/
    ports:
      PORT: 8000
    env:
      REDIS_URL: redis://127.0.0.1:${REDIS_PORT}/0

actions:
  - name: migrate
    key: m
    command: ./local/example/actions/migrate
```

Port names become environment variables shared by every service and action in that worktree. Assignments are globally unique and persisted between TUI sessions. Compose project names include a stable worktree identifier, so networks and volumes are isolated automatically.

## Keys

- `h/l`: move between project, worktree, and service panes
- `j/k`, `g/G`: navigate
- `enter` or `s`: start a service or run an action
- `x`, `r`: stop or restart the selected service
- `S`, `X`: start or stop all services in dependency order
- `:`: open an interactive shell in the selected worktree
- `n`: open the project plugin wizard
- `i`: inspect the selected worktree's identity, safe environment, services, and logs
- `R`: rediscover projects and worktrees
- `q`: close the TUI; supervised services continue running

Services run in stable tmux sessions. Logs and port state live under the macOS application-support directory, allowing the TUI to reconnect after it closes.

Projects without a plugin are hidden from the normal TUI. The `n` wizard can add and persist another discovery directory/repository, then create one `.runner/config.yaml` for that project. Its worktrees appear automatically. Invalid project plugins remain visible so their errors can be fixed.

## Plugin design

A plugin belongs to the project, not to an individual worktree. Commands run in the selected worktree, while ports, tmux sessions, Compose project names, logs, and runtime data are isolated per worktree. This makes the common workflow—switch worktree, start web, run a migration, inspect logs—available from one dashboard.

## Current boundary

The MVP intentionally does not remove Compose volumes. `Stop` preserves data. A future explicit `Clean` command will handle container, network, volume, and log removal with confirmation.
