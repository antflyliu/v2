# AGENTS.md

## Project Overview
**Miniflux v2** — minimalist feed reader written in Go 1.26.  
Core principle: **no bloat**, single static binary, PostgreSQL only.

## 1. Quick Start & Tooling (Windows 11)
- **Build**: `make miniflux` (PIE binary) or `go build -o miniflux.exe .`
- **Dev**: `.\dev++.ps1` (PowerShell script — supports `-WebOnly`, `-WorkerOnly`, `-Build`)
- **Lint**: `make lint` (gofmt + golangci-lint)
- **Test**: `make test` (or `go test ./... -race -cover`)
- **Debug**: VS Code tasks "go: build" / launch configs "Miniflux: Web + Worker"
- **Database**: PostgreSQL (native on Windows via WSL2 or Docker)

## 2. Architecture & Layering
- **internal/** — core business logic
  - `storage/` — CRUD layer (Feed, Entry, User, Category, Integration...)
  - `reader/` — fetcher, readability, cloudflare bypass, scraper rules
  - `http/` — HTTP server & middleware
  - `ui/` — web handlers & templates
  - `cli/` — command-line interface & daemon
  - `config/`, `database/`, `worker/`, `locale/`, `version/`
- **client/** — official Go client library for REST API
- **packaging/** — Docker, RPM, Debian, Systemd
- **contrib/** — 20+ integrations (Apprise, Linkding, Matrix, etc.)
- **vendor/** — all third-party dependencies (vendored)

## 3. Development & Quality Rules
- **Language**: Go 1.26 (no ORM, embed static assets)
- **Style**: gofmt/goimports mandatory; linter: errname, gocritic, staticcheck, loggercheck, goheader
- **Testing**: 80%+ coverage, table-driven tests, -race flag
- **Error Handling**: Always wrap errors with context (`%w`)
- **Windows Notes**: Use PowerShell + `.\dev++.ps1`; `Unblock-File` for execution policy

## 4. Safety & Workflow Guards
- **Never touch**: vendor/, .git/, .omc/, .cocoindex_code/, *.exe (except tmp/), .env.dev
- **Before edit**: check for sensitive data
- **After edit**: `make lint` → `make test` → `.\dev++.ps1 -WebOnly`

## 5. Known Gotchas
- Vendored dependencies tree
- Cross-platform builds (amd64/arm64/riscv64)
- Windows development uses PowerShell + PostgreSQL native
- Strict minimalist philosophy — no new features unless explicitly requested

**This AGENTS.md is the single source of truth for all future agent operations on this project.**