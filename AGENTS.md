# Repository Guidelines

## Project Structure & Module Organization

Piglala is a single-package Go Discord bot. Application code lives in the repository root: `main.go` wires configuration and Discord handlers, while focused files such as `fflogs.go`, `poller.go`, `store.go`, `music.go`, and `llama.go` own integrations and persistence. Tests sit beside their implementations as `*_test.go`. User-facing Discord responses are Go `text/template` files in `templates/`; keep their fixed filenames and supported data fields synchronized with `messages.go`. `lavalink/application.yml` and `compose.yaml` configure the local voice service. Runtime databases, logs, binaries, and `.env` are local artifacts and must not be committed.

## Build, Test, and Development Commands

- `cp .env.example .env` creates local configuration; fill in Discord and FFLogs credentials.
- `docker compose up -d lavalink` starts the voice node required for `!play`.
- `go run .` runs the bot locally and loads templates from `MESSAGE_TEMPLATE_DIR`.
- `go build -o piglala .` builds a local binary.
- `go test ./...` runs the complete test suite.
- `go vet ./...` checks common Go correctness issues.
- `gofmt -w *.go` applies the required Go formatting before review.

Use the Go version declared in `go.mod`. SQLite uses `go-sqlite3`, so builds require CGO and a C compiler.

## Coding Style & Naming Conventions

Follow standard Go conventions: tabs from `gofmt`, short lowercase package-level names, `MixedCaps` identifiers, and exported-name comments when adding public API. Keep integrations separated by concern rather than expanding `main.go`. Wrap errors with useful operation context and preserve existing structured log prefixes such as `discord:` and `store:`. Name templates in lowercase kebab case, for example `status-player.tmpl`.

## Testing Guidelines

Use Go’s `testing` package and name tests `TestBehavior`, with table-driven `t.Run` cases where inputs vary. Prefer `t.TempDir()` for SQLite files and local `httptest` servers for HTTP behavior; tests should not require real credentials or external services. Add regression coverage with every bug fix. Run `go test ./...` and `go vet ./...` before submitting.

## Commit & Pull Request Guidelines

Recent history favors concise imperative subjects with Conventional Commit prefixes, especially `feat:`, `fix:`, and `chore:`. Keep each commit focused. Pull requests should explain the user-visible change, list verification commands, link relevant issues, and call out configuration or database behavior changes. Include Discord output examples or screenshots when message templates or commands change, and never include tokens, `.env`, or runtime SQLite data.
