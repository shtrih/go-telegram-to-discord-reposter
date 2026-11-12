# AGENTS.md

Telegram → Discord reposter bot in Go (module name `reposter`). Listens to a Telegram channel via long-polling and forwards messages (text, media, formatting) to a Discord channel as embeds.

## Layout

- `main.go` — wiring: flags (`--config path`), config, Discord session, proxy-aware HTTP client, signal handling.
- `config/` — YAML config parsing (`config.example.yaml` shows the schema; `config.yaml` is local and gitignored-ish — never commit tokens).
- `handler/` — the core: `handler.go` converts Telegram updates into Discord embeds (formatting, link hiding, markdown translation). Most edit work happens here.
- `tgapi/` — Telegram-side helpers; `entities.go` converts Telegram message entities to Discord markdown. Has the only tests (`entities_test.go`).
- `dcapi/` — thin Discord session wrapper.
- `proxy/` — HTTP proxy support (relevant for Telegram API access from restricted networks).
- `database/` — GORM (SQLite) store of already-seen posts (`post_model.go`).

## Commands

- Build/check: `go build ./...` / `go vet ./...`
- Tests: `go test ./...` (focused: `go test ./tgapi`)
- Run: `go run . --config config.yaml`
- Docker: `docker-compose.yml` (builds via `Dockerfile`, expects `cnf.yaml` mounted).

## Conventions & gotchas

- Toolchain: `go 1.27.4` in go.mod; Docker build uses `golang:1.27-alpine`.
- Formatting conversion (Telegram entities → Discord markdown) is subtle: overlapping/nested entity ranges and link-hiding interactions have caused repeated bugs (see git history). When editing `tgapi/entities.go` or the formatting code in `handler/handler.go`, run `go test ./tgapi` and add a table test case for any new fix.
- Runtime logs go to stdout via `fmt`/bot library; keep it that way — no logging framework.
- Config fields are matched by YAML keys; keep `config.example.yaml` in sync when adding fields.
