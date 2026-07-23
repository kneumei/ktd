# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`ktd` is a personal work-todo tracker: a single native CLI binary with no
server component. Data lives as markdown files with YAML frontmatter in a
per-user data directory, version-controlled with plain git plumbing (`git
init`/`add`/`commit`/`push` shelled out to directly, not a git library).

## Build / run / check

```
go build ./...          # compile everything
go build -o ktd.exe .   # produce the ktd binary (see cmd/ktd)
go vet ./...
go run ./cmd/ktd <args>  # run without building, e.g. go run ./cmd/ktd list
```

There are no tests in the repo yet. If you add packages that warrant tests,
use standard `go test ./...` / `go test ./internal/store -run TestFoo`
conventions.

For manual end-to-end testing without touching the real data directory
(`%AppData%\kyle-to-do`), set `KTD_DATA_DIR` to point at a scratch directory,
e.g. the gitignored `testdata/` folder in this repo:

```
$env:KTD_DATA_DIR = "C:\Users\kneum\code\todo-cli\testdata"
go run ./cmd/ktd list
```

## Architecture

**Command dispatch** (`cmd/ktd/main.go`) is a hand-rolled switch over
`os.Args[1]` — no flag/cobra library. Flags (`--name`, `--name=value`,
boolean `--name`) are parsed by the local `parseArgs` helper, which — unlike
the stdlib `flag` package — allows flags to appear anywhere in the argument
list, not just before positionals (this matters for e.g.
`ktd close 0002 --as-of 2026-07-23`).

**Two classes of subcommand**, per the doc comment at the top of `main.go`:
- *Mechanical* (`list`, `close`, `context`, `sync`): pure, instant,
  no network call.
- *Fuzzy* (`add`, `done`, `edit`, `weekly`): make exactly one Anthropic API
  call to turn freeform text into structured data, using forced tool-use so
  the response is deterministic JSON rather than free text to scrape.

Package layout (`internal/`):
- **`model`** — the `Todo` struct (id, title, status, categories, created/
  closed dates, plan, links, body, log entries). Shared by every other
  package; has no logic beyond `IsOpen()`.
- **`store`** — data directory resolution (`KTD_DATA_DIR` env override, else
  `os.UserConfigDir()/kyle-to-do`), auto `git init` + `.gitignore` seeding on
  first run, and reading/writing `todos/*.md`. `frontmatter.go` holds the one
  true parse/serialize round-trip for the file format: YAML frontmatter in a
  fixed field order, then a freeform body, then an optional `## Log` section
  of `- YYYY-MM-DD: text` bullets. Every write funnels through
  `Serialize`/`Save`, so files are always re-canonicalized regardless of how
  they were originally formatted. `config.go` resolves the Anthropic API key
  in order: `KTD_ANTHROPIC_API_KEY` env var → `.env` in cwd → `.env` in the
  data dir.
- **`categories`** — categories are freeform, case-insensitive strings.
  `Build` tallies casing usage across all items and picks the most-used
  exact casing per lowercased category (ties broken alphabetically) as the
  canonical display form; every list/context/edit output goes through this
  before printing.
- **`resolve`** — mechanical (non-AI) lookup of an item by id or fuzzy text,
  used by `close`/`context`/`edit`. Order: exact id match (zero-padded to 4
  digits) first, then case-insensitive substring match against title,
  categories, and body. Never guesses on ambiguity — returns candidates for
  the caller to print and bail out on instead.
- **`ai`** — a minimal hand-written Anthropic Messages API client
  (`net/http`, no SDK — deliberate, to keep this a dependency-light CLI).
  `client.go` has the one HTTP call primitive (`CallTool`, forced
  `tool_choice`); `parse.go` has the three actual operations (`ParseAdd`,
  `ParseEdit`, `DraftWeekly`) plus deterministic (non-AI) URL extraction via
  `ExtractLinks`, which is always run before handing text to the AI so links
  don't clutter the classification prompt. Model is Claude Haiku 4.5
  (`ai.Model`), chosen for cost/speed on these small extraction tasks.
- **`commands`** — one function per subcommand, orchestrating the above
  packages. `confirm.go` centralizes the "print proposed item(s), prompt
  y/N" pattern shared by every AI-driven write (`add`/`done`/`edit`) —
  mechanical writes (`close`) don't prompt. `resolve.go` (in this package)
  wraps `internal/resolve` to also print match/candidate/no-match
  diagnostics to stdout, shared by `close`/`context`/`edit`.

## Data format

Each todo is one `todos/{id}-{slug}.md` file (4-digit zero-padded id; slug is
cosmetic only — the id is identity, and `Save` deletes the old file if the
title/slug changed). Frontmatter fields: `id`, `title`, `status`
(`open`/`closed`), `categories` (flow-style YAML list), `created`, `closed`,
`plan` (currently only meaningful value is `this-week`, read by `ktd
weekly`), `links` (one per line). Body is freeform markdown; an optional
trailing `## Log` section holds dated progress bullets.

## Git workflow

Commit directly to `main` by default. Don't create a feature branch or open
a PR for routine commits — only do that if explicitly asked to.

## Conventions worth knowing

- The Anthropic API key env var is `KTD_ANTHROPIC_API_KEY`, deliberately not
  the generic `ANTHROPIC_API_KEY` — so it never coincidentally picks up a key
  set for another tool (e.g. Claude Code itself) on the same machine.
- `testdata/` is gitignored — it's scratch data for manual testing via
  `KTD_DATA_DIR`, not fixtures.
- Windows is the primary dev target (`%AppData%`-based data dir), but the
  code is otherwise platform-generic (`path/filepath`, no Windows-only APIs)
  except for shelling out to `git`, which must be on `PATH`.
