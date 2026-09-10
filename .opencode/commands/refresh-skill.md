---
description: Regenerate and verify the immich-admin-cli agent skill (immich-admin-cli/SKILL.md) from the CLI's return-agent-skill command, fixing doc drift
agent: build
---

Regenerate the `immich-admin-cli` agent skill artifact and fix documentation drift.

## 1. Prerequisite check

Build the CLI and confirm the generator exists:

```sh
go build ./...
go run ./cmd/immich-admin return-agent-skill --help
```

If `return-agent-skill` does not exist (unknown command), STOP and tell the
user the `return-agent-skill` CLI command (`internal/commands/skill.go`)
must be implemented first — there is nothing to refresh yet.

## 2. Regenerate

Refresh via `go generate` (preferred — refreshes all generated files at
once, see §6). The skill generator needs no server connection and no
`--config`:

```sh
go generate ./...
```

Equivalent for a skill-only refresh:

```sh
go run ./cmd/immich-admin return-agent-skill --out immich-admin-cli/SKILL.md
```

The artifact directory must stay `immich-admin-cli` — the skill spec
requires it to match the skill `name`.

## 3. Verify

- Frontmatter: file starts/ends the header with `---`; `name` is
  `immich-admin-cli` (lowercase alphanumerics/hyphens, ≤64 chars, matches the
  parent directory); `description` is 1–1024 chars.
- Body is under 500 lines (warn if it exceeds the skill-spec recommendation).
- Summarize what moved: `git status --short immich-admin-cli/` and
  `git diff --stat immich-admin-cli/SKILL.md`.

## 4. Fix drift

Cross-check the regenerated catalog against the live CLI surface and the
hand-written sources:

- Run `--help` for each top-level command/group and compare: every command
  and flag in the skill catalog must exist, and every source example in
  `internal/commands/skill_best_practices.md` must reference real commands
  and flags.
- Stale command/flag references in examples → edit
  `internal/commands/skill_best_practices.md`.
- Missing or placeholder `Usage` strings → fix them in
  `internal/commands/*.go` (the catalog is generated from these, so this is
  the actual fix).
- After any source edit: re-run steps 2–3, then
  `go build ./...`, `go vet ./...`, and `go test ./internal/commands/`.

## 5. Adding best practices

Best practices are the only hand-written part of the skill. To add or change
them:

1. Edit `internal/commands/skill_best_practices.md` (Markdown, embedded
   into the binary via `//go:embed` in `internal/commands/skill.go`).
   What belongs here: cross-command flows, reusable patterns (ID plumbing,
   server-log triage), safety discipline. What does NOT belong here:
   per-command flag documentation — that lives in each command's `Usage`
   string (the catalog is generated from those, so fix inaccuracies there).
2. Re-run steps 2–3 to regenerate and verify.
3. Run `go test ./internal/commands/ -run Skill` — the embed/marker tests
   fail if the section is accidentally emptied or truncated.

Never hand-edit `immich-admin-cli/SKILL.md` itself: it is overwritten on
every regeneration and CI rejects manual drift.

## 6. How the generated files work

Three artifacts are generated, all refreshed by the single `go generate
./...` and all enforced fresh by CI (`.github/workflows/ci.yml` fails on
any diff):

| Artifact | Generator | Source of truth |
|----------|-----------|-----------------|
| `internal/immichapi/immichapi.gen.go` | oapi-codegen (pinned via `tool` directive in `go.mod`, driven by `internal/immichapi/generate.go`) | `api/immich-openapi-specs.json` |
| `README.md` API table (`API-TABLE` markers) | `tools/apitable` (scans spec + `internal/commands/`, `internal/workflows/`) | spec operationIds + command method names |
| `immich-admin-cli/SKILL.md` | `//go:generate` in `internal/commands/skill.go` → `return-agent-skill --out` | live command tree + `skill_best_practices.md` |

Workflow: change a source (spec, command, best practices) → run
`go generate ./...` → review the diff (`git status --short`, second run
must be a no-op) → run `go build ./... && go vet ./... && go test ./...` →
commit generated files together with the source change. Generated files are
never edited by hand.

## 7. Report

Report: which commands/flags were added, removed, or changed; validation
results (frontmatter, line count); regeneration idempotency (second run
clean?); the artifact path. Do NOT commit anything — committing,
branching, and PRs are the user's decision per repo rules.
