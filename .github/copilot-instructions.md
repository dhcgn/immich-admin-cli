# immich-admin-cli Project Guidelines

## Overview

This is a **Go CLI tool** for administering an Immich photo server. It enables bulk operations not available in the Immich web UI — compression, conversion, repair, and deletion of assets at scale.

## API Alignment

**All Immich API interactions MUST align with `api/immich-openapi-specs.json`.**

- Before implementing any API call, look up the operation in the OpenAPI spec by `operationId`, path, or tag.
- Use the exact request/response field names from the spec (they are camelCase JSON).
- Respect required vs. optional parameters as defined in the spec.
- Use the `x-immich-history` field to prefer stable (`"state": "Stable"`) endpoints over beta ones.
- **Never use deprecated endpoints.** An endpoint is deprecated if it has `"deprecated": true` at the operation level OR if `"Deprecated"` appears in its `tags` array. Use the `replacementId` in `x-immich-history` to find the replacement.
  - **One documented exception**: `updateAsset` (`PUT /assets/{id}`) is deprecated upstream with a self-referential `replacementId` (i.e. no real replacement exists) — it is the only Immich API that can set an asset's capture date. It is used deliberately and narrowly by `assets update` and `client-workflow fix-album-dates`. Do not use the bulk `updateAssets` variant or extend this exception to any other deprecated endpoint without the same "no alternative exists" verification.
- Authentication: the spec defines three security schemes — `bearer` (Authorization header), `cookie`, and `api_key` (x-api-key header). Prefer `api_key` for CLI usage.

## Language & Style

- **Go** — follow standard Go conventions (`gofmt`, `go vet`).
- Use `github.com/urfave/cli/v3` for CLI commands. Commands are grouped by OpenAPI spec tag as `<tag> <operation>` (e.g. `users me`) under `internal/commands/`, one file *prefix* per tag (`assets.go`, and `assets_download.go`, `assets_bulk.go` when a tag grows large); `cmd/` holds only the `main` package.
- Every command action starts with `newClient(cmd)` (from `internal/commands/helpers.go`) and checks responses with `client.Check(resp, http.StatusOK)` — never repeat config-loading or status-check boilerplate inline.
- Use `viper` for configuration (API URL, API key, etc.) in `internal/config/`.
- HTTP client: all API access goes through the thin wrapper in `internal/client/` (auth header injection, base URL); it wraps the generated `immichapi.ClientWithResponses`. Never construct `immichapi` clients or raw HTTP requests in command code.
- Request/response types: use the **generated** types from `internal/immichapi` — never hand-write DTOs that mirror the spec.
- Return errors; do not panic. Wrap errors with `fmt.Errorf("context: %w", err)`.
- Do not add global state; pass dependencies explicitly.

## Project Structure

```
immich-admin-cli/
├── cmd/
│   └── immich-admin/
│       └── main.go              # Binary entrypoint only: build the urfave/cli app and run it.
├── internal/                    # Private packages — not importable by other modules.
│   ├── commands/                # One file prefix per spec tag (users.go, assets.go, …)
│   │                            #   plus helpers.go (newClient, collectIDs, formatBytes).
│   ├── workflows/               # Client workflows: multi-step orchestrations (replace-asset,
│   │                            #   reencode-…) with shared step engine in workflow.go.
│   ├── client/                  # Hand-written thin wrapper around the generated client
│   │                            #   (auth, base URL, pagination helpers, error mapping).
│   ├── immichapi/               # GENERATED CODE — DO NOT EDIT. Regenerated from the spec.
│   │   ├── generate.go          #   //go:generate directive that drives oapi-codegen
│   │   │                        #   (pinned via the `tool` directive in go.mod).
│   │   └── immichapi.gen.go     #   Generated models + HTTP client (package immichapi).
│   └── config/                  # Viper configuration loading (server URL, API key).
├── api/                         # API contract & codegen tooling. BUILD INPUT — not compiled.
│   ├── immich-openapi-specs.json   #   Upstream spec (source of truth for all API calls).
│   ├── download.ps1                #   Refreshes the spec from immich-app/immich (main branch).
│   └── oapi-codegen-config.yml     #   oapi-codegen configuration.
├── tools/
│   └── apitable/                # Dev tool: regenerates the README "API Coverage" table
│                                #   from the spec (runs via go generate).
├── go.mod
├── go.sum
└── README.md
```

### Structural design decisions

- **Generated code is separated from the spec.** The OpenAPI JSON, the download
  script, and the codegen config are *build inputs* and live in `api/` (never
  compiled). The Go code produced from them is a real package and lives in
  `internal/immichapi/`, marked **DO NOT EDIT**.
- **Generated code lives under `internal/`** so it is not exposed as a public API
  and gets a clean import path (`.../internal/immichapi`), not a hyphenated
  sentence-shaped one.
- **The generated package is named `immichapi`, not `client`**, to avoid a name
  collision with the hand-written wrapper in `internal/client`. Application code
  never talks to `immichapi` directly — it goes through `internal/client`.
- **`cmd/` contains only `main` packages** (one subdirectory per binary). Command
  logic lives in `internal/commands/`.
- **Regeneration is reproducible.** `go generate ./...` rewrites
  `internal/immichapi/immichapi.gen.go` from `api/immich-openapi-specs.json`; the
  generated file is committed so a plain `go build` needs no code-gen tooling.

## Build & Test

```sh
pwsh api/download.ps1  # (optional) refresh the OpenAPI spec from upstream
go generate ./...      # regenerate the client (oapi-codegen) AND the README API coverage table (tools/apitable)
go build ./...
go test ./...
go vet ./...
go fix ./...    # check for outdated API usage that should be modernized
```

## Conventions

- CLI flags should mirror the parameter names from the OpenAPI spec where practical.
- **README API coverage table**: `README.md` contains a generated table of all spec operations and their implementation status (between `API-TABLE` markers). After implementing a new command, run `go generate ./...` to refresh it — CI fails if it is stale. Implementation status is **auto-detected** by scanning `internal/commands/` for generated method names; there is no manual list to maintain.
- **Base URL**: the config stores the server *origin* only (e.g. `https://immich.example.com/`); `internal/client` appends the `/api` base path declared in the spec's `servers` field. Never put `/api` in config values or operation paths.
- **Config files**: `config.example.yaml` is the committed template. Real configs (e.g. `config.prod.yaml`, the default for the `--config` flag) are gitignored because they contain the API key. Env vars `IMMICH_SERVER` / `IMMICH_API_KEY` override file values.
- UUIDs are `string` typed in Go (matching the spec's `format: uuid`).
- Pagination: the spec uses `page` / `pageSize` query params — always support pagination in list commands.
- Never hard-code the Immich server URL; always read it from config or a `--server` flag.

## Binary Downloads

**Endpoints returning `application/octet-stream` (e.g. `GET /assets/{id}/original`) MUST use the raw generated method, never the `...WithResponse` variant.** The `...WithResponse` variants buffer the entire body into memory (`Body []byte`), which is unacceptable for multi-GB assets.

```go
resp, err := c.API.DownloadAsset(ctx, id, params) // returns *http.Response, streaming body
// io.Copy(file, resp.Body) — stream to disk, defer resp.Body.Close()
```

The HTTP client deliberately has **no overall request timeout** (long downloads are legitimate); it fails fast via connection-setup and response-header timeouts instead. Do not add `http.Client.Timeout`.

## Bulk Operations

For endpoints taking ID arrays (e.g. `DELETE /assets` with `AssetBulkDeleteDto`, `PUT /assets/copy`):

- Accept IDs via `--ids-file <path>` (shared `idsFileFlag()` + `collectIDs()` helpers) — one UUID per line, blank lines and `#` comments skipped; `-` means stdin (enables piping between commands). Positional args and `--ids-file` combine.
- Commands that fan out over **single-ID endpoints** (e.g. `assets info` → `GET /assets/{id}`) iterate sequentially, continue on per-ID errors (logged to stderr), and return a summary error at the end so the exit code reflects partial failure.
- **Chunk** large ID lists into batches (default 500 per request); never send one huge array.
- **Destructive commands** (delete, trash, …) must support `--dry-run` (print what would happen) and require `--yes` to skip the confirmation prompt. Map the spec's `force` field to a `--force` flag.

## Client Workflows (custom orchestrations)

Client workflows are the tool's **main purpose**: multi-step operations that combine several API calls with local processing into one command (e.g. replace an asset, re-encode to JPEG XL). They run entirely client-side.

- **Naming**: the CLI group is **`client-workflow`** (alias `cw`), subcommands kebab-case (`replace-asset`, `reencode-jxl`, `reencode-jpegli`). ⚠️ Do NOT use the group name `workflows` — that plural is reserved for the Immich API's own server-side "Workflows" tag (`/workflows/...`), which is an entirely different concept.
- **Architecture**: workflow logic lives in `internal/workflows/` — one file per workflow plus `workflow.go` holding the shared engine. A workflow is an ordered list of named steps executed per asset; the engine handles step logging, `--dry-run` (print the steps, execute nothing), and the per-asset continue-on-error loop with summary exit code (same bulk conventions and `collectIDs` input handling as commands). The command layer stays thin: `internal/commands/clientworkflow.go` only maps CLI flags to workflow options.
- **Safety invariants (MUST hold for every workflow)**:
  - The destructive step is always the **last** step.
  - The original asset is removed only after the replacement is **verified** (new asset exists and checksum matches).
  - Removal goes to **trash** by default (restorable); permanent deletion only behind `--force`.
  - A failed step aborts that asset's workflow and leaves the original untouched; the batch continues with the next asset.
  - `--dry-run` and `--yes` are required per the Bulk Operations conventions.
- **Local processing**: external encoders (`cjxl`, `cjpegli`, …) are invoked via `os/exec`. Binary paths come from an optional `tools:` section in the config file, falling back to `PATH` lookup. Check encoder availability **once before the batch starts** (fail fast, not at asset #500). Temp files go into a per-run temp directory and are cleaned up afterwards. Re-encoding must preserve EXIF (use the encoders' metadata-preservation flags).
- **API access rule**: `internal/workflows` calls the API exclusively through the `internal/client` wrapper — same rule as commands; never raw HTTP, never direct `immichapi` client construction.
- **Expose the underlying API operations too**: when a client workflow drives one or more Immich API operations directly, also surface those operations as plain commands in the matching `<tag>` command group (`internal/commands/<tag>.go`, grouped `<tag> <operation>`). A workflow is a convenience orchestration on top of the raw operations, not a replacement for them — users should still be able to invoke each operation standalone. Example: the `client-workflow tag-delete` workflow (using `getAllTags` + `deleteTag`) ships alongside a `tags` command group exposing `tags list` (`getAllTags`), `tags get` (`getTagById`) and `tags delete` (`deleteTag`). Each such command follows the normal command conventions (thin action, `newClient`, `client.Check`, bulk `--dry-run`/`--yes`/`--ids-file` where destructive), and both the command and the workflow count toward the README API-coverage table.

## Code Generation Behavior

How the two generators work — important when touching the spec, the generated client, or the README:

- **Toolchain pinning**: the `oapi-codegen` CLI is pinned via the **`tool` directive in `go.mod`** and invoked as `go tool oapi-codegen` (see `internal/immichapi/generate.go`). Do NOT remove this directive and do NOT expect `go mod tidy` to manage it from imports — nothing imports the CLI, and without the `tool` directive tidy silently drops the dependency and `go generate` breaks (this happened once).
- **Generated method naming** (oapi-codegen): PascalCase of the spec's `operationId` — `getMyUser` → `GetMyUser` / `GetMyUserWithResponse`. **Exception**: operations with form-data/multipart request bodies (e.g. `uploadAsset`, `createProfileImage`) get only `...WithBody` / `...WithFormdataBody` variants — a plain `UploadAsset(` method does not exist. Full pattern: `<PascalOpId>(WithBody|WithFormdataBody)?(WithResponse)?(`.
- **`tools/apitable`** rewrites the README section between `<!-- API-TABLE:BEGIN -->` / `<!-- API-TABLE:END -->` markers:
  - An operation counts as **implemented** when its method-name pattern (above) matches anywhere in `internal/commands/*.go` or `internal/workflows/*.go` (tests excluded) — an operation used only inside a client workflow still counts. There is no manual mapping; putting API calls anywhere else will NOT be detected (another reason all calls belong in those packages via the client wrapper).
  - **Deprecated and Internal operations are excluded** from the table (per `deprecated: true`, tag `Deprecated`, or `x-immich-state` ∈ {Deprecated, Internal}) and only counted in the summary line.
  - It prints a **naming-drift warning** to stderr when a spec operationId has no matching generated method — investigate warnings; they mean the naming convention above no longer holds.
  - Output is deterministic (tags alphabetical, rows by path/method), so re-running never causes diff churn.
- **CI enforces freshness** of BOTH outputs: it runs `go generate ./...` and fails on any diff in `internal/immichapi/` or `README.md`. Therefore: after implementing a command or updating the spec, always run `go generate ./...` and commit the resulting changes together.

## Tooling

- The **GitHub CLI (`gh`) is installed** — use it for GitHub operations: creating releases and release notes (`gh release create`), watching workflow runs (`gh run watch`, `gh run list`), PRs (`gh pr create`), and API queries (`gh api`). Prefer `gh` over raw git remotes or manual web-UI steps.

## Versioning & Releases

- Version, commit, and build date are injected at build time via `-ldflags "-X main.version=… -X main.commit=… -X main.date=…"` into `cmd/immich-admin/main.go`; shown by `immich-admin --version`.
- CI (`.github/workflows/ci.yml`): fmt/build/vet/test + generated-code freshness check on pushes/PRs to `main` and `dev`.
- Releases (`.github/workflows/release.yml`): a `v*` tag on `main` produces a stable release; every push to `dev` produces a beta prerelease versioned `<latest-tag>-beta.<run-number>`. Binaries are cross-compiled for linux/darwin/windows.

## Branching & Release Workflow

**Never assume an implementation task should end in a PR.** Only create branches, commit, push, or open a PR when the user has explicitly asked for that (or it's unambiguous from context, e.g. they asked to "ship"/"release" something). Otherwise implement and validate the change in the working tree/current branch and leave committing/branching/PR creation to the user. If it's unclear whether a PR is wanted, ask before pushing or opening one.

Flow: **feature branch → PR into `dev` (beta) → PR `dev` into `main` + tag (stable)**.

Rules:

- **Branch from `dev`, never from `main`** — `main` may lag behind. Feature branches are named `feature/<name>` and deleted at merge; `dev` and `main` are permanent.
- **The tag makes the stable release, not the merge.** Merging `dev`→`main` only runs CI; pushing a `v*` tag on `main` triggers the Release workflow.
- Before every push, run the local CI equivalent: `go generate ./... && go build ./... && go test ./... && go vet ./... && go fix ./...` — and commit whatever `go generate` changed (README table, generated client), or CI's freshness check fails.
- If `main` ever receives a direct hotfix, sync it back: `git checkout dev && git merge origin/main && git push`.

### 1. Start a feature (from fresh `dev`)

```sh
git checkout dev && git pull origin dev
git checkout -b feature/<name>
# work, commit, then local CI check (see rules above)
```

### 2. Feature → dev (triggers beta prerelease)

```sh
git push -u origin feature/<name>
gh pr create --base dev --title "<title>"
gh pr checks --watch                 # wait for CI
gh pr merge --merge --delete-branch  # merge push to dev builds v<latest>-beta.N
gh run list --branch dev --limit 2   # verify Release run; gh release list shows the beta
```

Repeat 1–2 per feature; each merge to `dev` produces a new beta to test.

### 3. dev → main + tag (stable release, when betas are good)

```sh
git checkout dev && git pull
gh pr create --base main --head dev --title "Release vX.Y.Z"
gh pr checks --watch
gh pr merge --merge                  # do NOT delete dev
git fetch origin main
git tag vX.Y.Z origin/main           # tag the merge commit on main
git push origin vX.Y.Z              # ← THIS triggers the stable release
gh release view vX.Y.Z               # verify; polish notes: gh release edit vX.Y.Z --notes-file <file>
```

After tagging `vX.Y.Z`, subsequent dev betas automatically become `vX.Y.Z-beta.N`.

