# Plan: `client-workflow fix-album-dates`

## Summary
New client workflow that scans all albums, auto-detects ones whose name starts with a date
(`yyyy-MM-dd <title>` e.g. "2025-07-04 Garten", or `yyyy <title>` e.g. "2010 USA"), fetches each
matched album's assets via album-scoped search, flags any asset whose `LocalDateTime` falls outside
the date range implied by the album name, and (for exact-day albums only) offers to fix mismatches by
writing a corrected date via `PUT /assets/{id}` (`updateAsset`).

## User decisions (locked in)
1. **Deprecated endpoint**: `updateAsset`/`updateAssets` are the ONLY endpoints in the vendored spec
   that can set an asset's capture date, and both are `deprecated: true` with a self-referential
   (non-existent) `replacementId`. Exhaustively verified (all 4 occurrences of `dateTimeOriginal`-like
   fields in the whole spec checked: `UpdateAssetDto`/`AssetBulkUpdateDto` = deprecated writes;
   `ExifResponseDto` = read-only; `SyncAssetExifV1` = orphaned schema, zero operations reference it).
   `editAsset`/`AssetEditsCreateDto` (newest, Beta, non-deprecated) is crop/rotate/mirror only, no date
   field. `updateAssetMetadata`/`AssetMetadataUpsertDto` (newest, Stable, non-deprecated) is a generic
   free-form `key: string, value: object` store, not a typed date-field replacement. docs.immich.app
   pages for update-asset(s) 404. No GitHub evidence of a replacement found.
   **Decision: use `updateAsset` (single-asset PUT /assets/{id}), NOT the bulk variant.** This is a
   deliberate, documented, narrow exception to the repo's "never use deprecated endpoints" rule.
2. **Year-only albums** (e.g. "2010 USA"): report-only. No unambiguous single date exists to reset
   outliers to, so these are detected/listed but never auto-fixed.
3. **Command name**: `fix-album-dates` (under `client-workflow`, i.e. `immich-admin client-workflow
   fix-album-dates` / `immich-admin cw fix-album-dates`).

## Architecture / templates used
- Workflow engine: `internal/workflows/workflow.go` — `Step`/`RunSteps` (dry-run + stop-on-first-error,
  used per-asset with a single destructive step), `RunBatch` (continue-on-error + summary error, used
  across mismatched assets).
- Closest template: `internal/workflows/adduserstoalbum.go` + its command
  `internal/commands/clientworkflow.go#clientWorkflowAddUsersToAlbumWithPattern` /
  `addUsersToAlbumWithPatternCommand` — same shape: fetch all albums (`GetAllAlbumsWithResponse` with
  empty `&immichapi.GetAllAlbumsParams{}`), pure filter function, print review list, bulk confirm,
  `RunBatch` to mutate. `tagdelete.go` is the simplest reference for the read/filter/mutate split and
  its test (`tagdelete_test.go`, `tagdelete_integration_test.go`).
- **Key discovery (verified directly in `internal/immichapi/immichapi.gen.go` + the spec, contradicts
  initial assumption)**: `AlbumResponseDto` (returned by both `GetAllAlbumsWithResponse` and
  `GetAlbumInfoWithResponse`) has **no `Assets` field** in this API version. Album assets must be
  fetched via `POST /search/metadata` (`searchAssets`, Stable) with `MetadataSearchDto.AlbumIds` set to
  the album ID, paginated via `Page`/`Size`/response `NextPage` cursor — exactly the pattern already
  implemented in `internal/commands/search.go#searchMetadata` (loop until `NextPage` is nil/empty,
  `resp.JSON200.Assets.Items`/`.NextPage`). The new workflow reimplements this loop inside
  `internal/workflows` (can't import `internal/commands`), calling `c.API.SearchAssetsWithResponse`
  directly.
- Relevant DTO fields (all confirmed directly in `internal/immichapi/immichapi.gen.go`):
  - `AssetResponseDto.LocalDateTime time.Time` — "local date and time... used for timeline grouping by
    local days" — the correct field to compare against an album's implied local calendar date/year
    (NOT `FileCreatedAt`, which is a UTC instant and can land on a different calendar day depending on
    timezone; NOT what `printAsset` in `assets.go` shows, which uses `FileCreatedAt` for a different,
    general-info purpose — deliberate deviation, worth a one-line comment).
  - `UpdateAssetDto.DateTimeOriginal *string` — the only settable field we need; all other
    `UpdateAssetDto` fields are left nil (it's a partial-update DTO, matching how `AssetBulkUpdateDto`
    is already documented/used elsewhere).
  - `MetadataSearchDto.AlbumIds *[]openapi_types.UUID`, `.Page *int`, `.Size *int`.

## Steps

### 1. Branch (do first)
`git checkout dev && git pull origin dev && git checkout -b feature/fix-album-dates` per
`.github/copilot-instructions.md`'s branching workflow (branch from `dev`, never `main`).

### ADDED BY USER

Call api\download.ps1 and update with this the api\immich-openapi-specs.json, review the changes if they could affect the plan. Ask User if something is unclear because of these changes, and should be re-considered in the plan. If nothing is unclear, continue with the plan.

### 2. Workflow implementation — `internal/workflows/fixalbumdates.go` (new file)
*depends on 1*
- `PatternKind` string enum: `PatternDay`, `PatternYear`.
- `AlbumDatePattern{Kind PatternKind; From, To time.Time}` — `To` is exclusive end of range.
- `parseAlbumDatePattern(name string) (AlbumDatePattern, bool)` — **unexported**, pure (matches
  visibility convention of `filterTags`/`filterAlbums`/`matchUsers`). Try day regex first
  (`^(\d{4}-\d{2}-\d{2})(?:\s+.*)?$`, parsed with `time.ParseInLocation("2006-01-02", m[1], time.UTC)`,
  `To = From.AddDate(0,0,1)`), rejecting invalid calendar dates (e.g. "2025-13-40") by treating a parse
  error as "no match" (not an error — album simply isn't date-pattern). Then year regex
  (`^(\d{4})(?:\s+.*)?$`, `To = From.AddDate(1,0,0)`). Order matters: a day-pattern name like
  "2025-07-04 Garten" must not spuriously match the year regex (verified it doesn't: char after the 4
  digits is `-`, not whitespace/EOS, so the year regex's own anchor already prevents this — the
  day-first ordering is just defense in depth).
- `AlbumDateCheck{Album immichapi.AlbumResponseDto; Pattern AlbumDatePattern; Mismatches
  []immichapi.AssetResponseDto}`.
- `fetchAlbumAssets(ctx, c, albumID) ([]immichapi.AssetResponseDto, error)` — unexported, paginated
  `SearchAssetsWithResponse` loop scoped to one album (see Architecture note above).
- `findDateMismatches(assets []immichapi.AssetResponseDto, p AlbumDatePattern)
  []immichapi.AssetResponseDto` — unexported, pure: keeps assets where `LocalDateTime.Before(p.From) ||
  !LocalDateTime.Before(p.To)`.
- `CheckAlbumDates(ctx, c, opts FixAlbumDatesOptions) ([]AlbumDateCheck, error)` — **exported**
  (network read entry point, mirrors `SelectAlbumsForSharing`/`SelectTagsForDeletion`): fetch all
  albums, keep ones where `parseAlbumDatePattern` matches, fetch+check each, return one
  `AlbumDateCheck` per matched album (including ones with zero mismatches — caller decides what to
  print/count).
- `computeFixedDateTime(asset immichapi.AssetResponseDto, p AlbumDatePattern) (time.Time, bool)` —
  unexported, pure: only for `PatternDay` (`ok=false` for `PatternYear`, per locked decision #2);
  returns the album's Y-M-D combined with the asset's *existing* `LocalDateTime` H:M:S/location
  (preserves relative ordering within the album; least destructive correction).
- `FixAlbumDatesOptions{DryRun bool}`.
- `FixAlbumDates(ctx, c, checks []AlbumDateCheck, opts FixAlbumDatesOptions) error` — exported mutate
  entry point. For each check: if `Pattern.Kind != PatternDay`, skip with an informational message
  (not a failure). Else `RunBatch` over `Mismatches`, each item wrapped in a single-step `RunSteps`
  call (`"Set date of <OriginalFileName> to <fixed date> (was <old LocalDateTime>)"`) invoking
  `updateAssetDate`.
- `updateAssetDate(ctx, c, id openapi_types.UUID, t time.Time) error` — unexported; calls
  `c.API.UpdateAssetWithResponse(ctx, id, immichapi.UpdateAssetDto{DateTimeOriginal: &s})` +
  `client.Check(resp, http.StatusOK)`, where `s = t.Format(...)`. **Doc comment must state plainly**
  that this is the repo's one deliberate exception to "never use deprecated endpoints" and link the
  rationale (no replacement exists — see spec analysis above). Format: use a naive local timestamp
  string with no timezone suffix (e.g. `"2006-01-02T15:04:05"`), matching EXIF `DateTimeOriginal`'s
  own timezone-less semantics — **flag for manual verification against a real server** (the request
  schema is an unconstrained `type: string`, no documented format/pattern, so the exact string Immich
  expects isn't 100% certain from the spec alone).

### 3. Workflow unit tests — `internal/workflows/fixalbumdates_test.go` (new file)
*depends on 2, parallel with 4/5*
Table-driven, pure, no network (mirrors `tagdelete_test.go` style):
- `parseAlbumDatePattern`: exact day match, day+title match, exact year match, year+title match,
  invalid calendar date (e.g. "2025-13-40 Foo") → no match, unrelated name → no match, day-pattern
  name does NOT get misclassified as year-pattern.
- `findDateMismatches`: asset inside range (kept out of result), asset exactly at `From` (in range),
  asset exactly at `To` (out of range — exclusive end), asset before/after range.
- `computeFixedDateTime`: day-kind preserves H:M:S and changes Y-M-D; year-kind returns `ok=false`.

### 4. Integration test (read-only) — `internal/workflows/fixalbumdates_integration_test.go` (new file)
*depends on 2, parallel with 3*
Mirrors `tagdelete_integration_test.go`: `//go:build integration`, skip if `../../config.prod.yaml`
missing, calls only `CheckAlbumDates` (never `FixAlbumDates`) against a real server, sanity-checks
results.

### 5. Command layer — `internal/commands/clientworkflow.go` (edit)
*depends on 2*
- Add `fixAlbumDatesCommand()` to the `Commands` slice in `ClientWorkflow()`.
- `fixAlbumDatesCommand()`: `Name: "fix-album-dates"`, flags `--dry-run`, `--yes` (same minimal set as
  `tag-delete`; no `--interactive`/`--include`/`--exclude` — see Excluded scope).
- `clientWorkflowFixAlbumDates(ctx, cmd)`: `newClient` → `workflows.CheckAlbumDates` → print per-album
  report for every check that has `len(Mismatches) > 0` (album name/id, pattern range, and for each
  mismatched asset: id, filename, current `LocalDateTime`, and — for day-kind only — the computed fix
  date; year-kind mismatches print "report only, no automatic fix") → if there are zero fixable
  (day-kind) mismatches, print summary and return nil (nothing to confirm) → print the one-line
  deprecated-endpoint warning → if `!dryRun && !yes`, bulk confirm ("Fix N asset(s) across M album(s)...
  [y/N]") → `workflows.FixAlbumDates`.

### 6. Expose `updateAsset` as a plain command — `internal/commands/assets.go` (edit)
*depends on none, parallel with 2-5*
Per the repo's "expose the underlying API operations too" convention (workflow drives an operation
directly → also ship it standalone). Add `assets update`:
- Bulk fan-out over IDs using existing `idsFileFlag()`/`collectIDs()` (same continue-on-error +
  summary-error loop already used by `assetsInfo` in this file).
- Flags mirroring `UpdateAssetDto` fields: `--date-time-original`, `--description`, `--is-favorite`
  (tri-state via the existing unexported `setBoolFlag` helper from `search.go`, same package), `--latitude`,
  `--longitude`, `--live-photo-video-id`, `--rating`, `--visibility`.
- Usage/Description text and a doc comment must clearly flag this as wrapping a deprecated upstream
  endpoint (the repo's one documented exception).
- Deliberately **not** adding `assets bulk-update` (`updateAssets`) — out of scope, see Decisions.

### 7. Documentation
*depends on 5, 6*
- `.github/copilot-instructions.md`: amend the "Never use deprecated endpoints" bullet (line 15) to
  note the one approved, narrow exception (`updateAsset`, via `assets update` and `client-workflow
  fix-album-dates`), with a one-line rationale, so it isn't mistaken for an oversight later.
- `README.md`:
  - Add a row to the Client Workflows status table (✅ done, `client-workflow fix-album-dates`,
    description).
  - Add a `### client-workflow fix-album-dates` subsection (own numbered steps + the deprecated-endpoint
    callout + example invocations), following the exact style of the existing `tag-delete`/
    `add-users-to-album-with-pattern` subsections.
  - Optional: one new "## Sample Use Cases" entry using the user's own example album names
    ("2025-07-04 Garten", "2010 USA"), mirroring the existing replace-asset walkthrough style.
- Run `go generate ./...` (regenerates the API-coverage table; expected to be a no-op or near-no-op
  since deprecated operations are excluded from the table regardless of implementation status — still
  required so CI's freshness check passes).

### 8. Local CI equivalent (final step)
*depends on all above*
`go generate ./... && go build ./... && go test ./... && go vet ./... && go fix ./...`, commit whatever
`go generate` changed.

## Relevant files
- `internal/workflows/workflow.go` — `Step`, `RunSteps`, `RunBatch` (reuse, no changes).
- `internal/workflows/adduserstoalbum.go` — primary structural template.
- `internal/workflows/tagdelete.go` + `tagdelete_test.go` + `tagdelete_integration_test.go` — simplest
  read/filter/mutate + test-layout template.
- `internal/workflows/fixalbumdates.go`, `fixalbumdates_test.go`, `fixalbumdates_integration_test.go` —
  new.
- `internal/commands/clientworkflow.go` — add `fixAlbumDatesCommand()` +
  `clientWorkflowFixAlbumDates`; reuse `confirm()`.
- `internal/commands/assets.go` — add `assets update` (`assetsUpdateCommand()`/`assetsUpdate()`),
  reuse `idsFileFlag()`/`collectIDs()`/`setBoolFlag()` (the last defined in `search.go`, same package).
- `internal/commands/helpers.go` — reuse `newClient`, `idsFileFlag`, `collectIDs` (no changes).
- `internal/immichapi/immichapi.gen.go` — **DO NOT EDIT** (generated). Confirmed exact symbols to use:
  `GetAllAlbumsWithResponse`, `SearchAssetsWithResponse`/`MetadataSearchDto`/`SearchAssetsParams`,
  `UpdateAssetWithResponse`/`UpdateAssetDto`, `AssetResponseDto.LocalDateTime`.
  `internal/immichapi/generate.go` has the only other `//go:generate` besides `tools/apitable/main.go` —
  neither needs changes.
- `.github/copilot-instructions.md` — amend deprecated-endpoints bullet (line 15).
- `README.md` — Client Workflows table + new subsection + API coverage table (auto).

## Verification
1. `go build ./...`, `go vet ./...` — compiles cleanly.
2. `go test ./...` — new `fixalbumdates_test.go` cases pass (pattern parsing edge cases, boundary
   mismatch detection, fix-date computation).
3. `go generate ./...` then `git diff --stat` — confirm README/generated-client diff is empty or
   expected only (no stray changes).
4. Manual smoke test with `--dry-run` against a real server (`config.prod.daniel.yaml` or
   `config.prod.julia.yaml` exist in the repo root): confirm it correctly identifies albums matching
   both patterns and reports mismatches without writing anything.
5. **Caution**: before testing the actual fix (non-dry-run) against real data, verify the exact
   `dateTimeOriginal` string format Immich expects (naive local, no `Z`/offset — per the implementation
   note in step 2) against a disposable/test album first, since the request schema doesn't pin the
   format and a wrong format could silently shift dates by a timezone offset.
6. Optional: `go test -tags integration ./internal/workflows/ -run FixAlbumDates` if a generic
   `config.prod.yaml` is created (currently only the personal `config.prod.daniel.yaml` /
   `config.prod.julia.yaml` exist, so this is skipped by default in this environment).

## Decisions
- Use `updateAsset` (single-asset), not `updateAssets` (bulk) — user's explicit choice.
- Year-only albums are report-only, never auto-fixed — user's explicit choice.
- Command name `fix-album-dates` — user's explicit choice.
- Comparison field is `LocalDateTime` (tz-agnostic local capture day), not `FileCreatedAt` (UTC
  instant) — chosen because Immich's own field description says it's "used for timeline grouping by
  local days", which is exactly this workflow's semantics; differs from `assets info`'s display field
  choice deliberately.
- No `--include`/`--exclude`/`--interactive`/`--json` flags for `fix-album-dates` — the user's request
  is "scan everything, auto-detect the pattern"; keeping the flag surface at parity with the simplest
  existing workflow (`tag-delete`: just `--dry-run`/`--yes`). Easy to add later if wanted.
- Not exposing `assets bulk-update` (`updateAssets`) — only the single-asset `updateAsset` that the
  workflow actually uses gets exposed, to avoid widening the deprecated-endpoint exception beyond
  what's needed.
- Album date parsing requires the dash-separated `yyyy-MM-dd` format exactly as specified by the user
  (no support for `.`/`_` separators or other layouts) — not requested, avoids over-engineering.

## Further Considerations
1. Exact `dateTimeOriginal` request string format is unverified against a live server (spec has no
   `pattern`/`format` on the request-side field) — flagged as a manual-verification step rather than a
   blocking question, since it's a fast check once implementation starts.
2. `--include`/`--exclude` regex filters (consistent with other workflows) or a `--album-id` scoping
   flag (like `repair-assets`) could be added later if scanning every album each run is undesirable at
   large scale — deliberately excluded from this pass per the user's "scan everything" request.
