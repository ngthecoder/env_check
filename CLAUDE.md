# CLAUDE.md — envcheck

## What is this?

A Go server + web dashboard that scans what's installed on the local machine and displays it in a live dashboard. Think "system inventory" — not project dependencies, but **what tools, runtimes, and packages exist on this computer**.

## Project Goal

Answer the question: "What do I have installed on this machine?"

### In scope (system-level)
- **Runtime version managers**: pyenv, goenv, nvm, fnm, rbenv, jenv, rustup, asdf, mise — what versions are installed and which is active
- **System package managers**: brew (macOS/Linux), apt/dpkg (Debian/Ubuntu), snap, flatpak, pacman (Arch)
- **Global packages**: pip (global site-packages), npm -g, cargo install, gem, go install binaries
- **CLI tools in PATH**: what commands are available, where they resolve to
- **System info**: OS, arch, kernel, shell, hostname

### Out of scope (project-level — do NOT add)
- Local project dependencies (go.mod, package.json, Cargo.toml, requirements.txt)
- Per-project virtual environments
- Docker containers or images

## Architecture

```
envcheck-server/
├── cmd/
│   ├── main.go              # Entry point, CLI flags, embed dashboard
│   └── web/
│       └── index.html       # Self-contained dashboard (embedded into binary)
├── internal/
│   ├── collector/
│   │   └── collector.go     # Scans the system for installed tools
│   └── server/
│       └── server.go        # HTTP server + SSE + file watcher
├── go.mod
├── Makefile
├── Dockerfile
├── envcheck@.service        # systemd unit file
└── CLAUDE.md                # (this file)
```

## Tech Stack

- **Go 1.22+** — single binary, no runtime deps
- **embed.FS** — dashboard HTML is compiled into the binary
- **fsnotify** — watches version files for changes
- **SSE (Server-Sent Events)** — pushes updates to dashboard in real-time
- **Vanilla HTML/CSS/JS** — no React, no build step for the dashboard

## How to build & run

```bash
make run          # build + start server on :8484
make dev          # same but with 5s scan interval
make json         # one-shot JSON output to stdout
```

## API

- `GET /` — dashboard
- `GET /api/status` — latest scan as JSON
- `POST /api/rescan` — trigger manual rescan
- `GET /api/events` — SSE stream of scan updates

## Key Design Decisions

1. **Single binary** — `go:embed` bundles the dashboard HTML, no separate file serving needed
2. **Parallel collection** — each collector runs in its own goroutine
3. **SSE not WebSocket** — simpler, one-directional updates are all we need
4. **Debounced file watching** — don't rescan more than once per 2s on file changes
5. **Graceful degradation** — if a tool isn't installed, skip it silently

## JSON Output Schema

```json
{
  "timestamp": "2026-02-06T...",
  "hostname": "my-machine",
  "os": "darwin",
  "arch": "arm64",
  "shell": "zsh",
  "system": {
    "os_name": "macOS 15.2",
    "kernel": "24.2.0",
    "uptime": "3d 4h"
  },
  "runtimes": [
    {
      "manager": "pyenv",
      "language": "python",
      "active_version": "3.12.1",
      "global_version": "3.11.7",
      "installed_versions": ["3.9.18", "3.11.7", "3.12.1"]
    }
  ],
  "packages": [
    {
      "manager": "brew",
      "total": 42,
      "packages": [{"name": "git", "version": "2.43.0"}, ...]
    },
    {
      "manager": "pip",
      "scope": "global",
      "total": 15,
      "packages": [{"name": "numpy", "version": "1.26.3", "latest": "1.26.4"}, ...]
    }
  ],
  "cli_tools": [
    {"name": "docker", "path": "/usr/local/bin/docker", "version": "24.0.7"},
    {"name": "kubectl", "path": "/usr/local/bin/kubectl", "version": "1.29.0"}
  ]
}
```

## Coding Conventions

- Keep collectors independent — each should handle its own errors and return nil if the tool isn't found
- Use `run()` helper for shell commands, never `os/exec` directly
- All collectors go in `internal/collector/collector.go` (or split into files if it gets big)
- Dashboard is a single `index.html` — inline CSS + JS, no build tools
- Dashboard uses CSS variables for theming (dark mode, monospace font)
- Japanese comments are fine

## What to work on next

- [ ] Add system info collection (OS, arch, kernel, uptime)
- [ ] Add `apt`/`dpkg` collector for Debian/Ubuntu
- [ ] Add `snap` and `flatpak` collectors
- [ ] Add CLI tool discovery (scan PATH for known tools, get their versions)
- [ ] Remove project-level dependency scanning (go.mod, package.json local deps)
- [ ] Add `--watch-dir` flag to specify which directories to watch
- [ ] Dashboard: add a "System" tab showing OS info
- [ ] Dashboard: add a "CLI Tools" tab showing what's in PATH
