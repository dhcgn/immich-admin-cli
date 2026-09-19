---
name: immich-admin-cli
description: Manage an Immich photo server from the command line: manage assets, albums, tags, and users; search by metadata; download originals and thumbnails; upload files; inspect server workflows and their run logs; and run client-side bulk workflows that find and repair corrupt photos, replace assets, re-encode media, and mirror albums locally. Use when working with Immich photo libraries, thumbnails, metadata, bulk tagging, or photo backup automation.
compatibility: Requires the immich-admin CLI binary and network access to an Immich server v3.2.0 or newer.
---

The `immich-admin` CLI manages an Immich photo server from the command line. Configure it with `--config FILE` (default `config.prod.yaml`) or the `IMMICH_SERVER` / `IMMICH_API_KEY` env vars; the server must be v3.2.0 or newer.

## Commands

Run `immich-admin <command path> --help` for full details and examples.

## assets

Asset operations

### assets info

Show information about one or more assets (GET /assets/{id})
Args: `[ASSET_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--json` (bool): print the raw responses as a JSON array

### assets update

⚠ DEPRECATED upstream: update one or more assets (PUT /assets/{id})
Args: `[ASSET_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--date-time-original` (string): set the asset's original date and time
- `--description` (string): set the asset description
- `--is-favorite` (string): mark as favorite: true or false
- `--latitude` (float): set the latitude coordinate [default: 0]
- `--longitude` (float): set the longitude coordinate [default: 0]
- `--live-photo-video-id` (string): set the live photo video asset `UUID`
- `--rating` (int): set the rating: 1-5 (starred) or -1 (rejected) [default: 0]
- `--visibility` (string): set visibility: archive, hidden, locked, or timeline

### assets download-original

Download original asset files (GET /assets/{id}/original)
Args: `[ASSET_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--out-dir` (string): directory to save downloaded files into (default: current working directory)
- `--quiet` (bool): disable per-file progress bars on stderr

### assets download-thumbnail

Download thumbnail/preview images for asset files (GET /assets/{id}/thumbnail)
Args: `[ASSET_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--out-dir` (string): directory to save downloaded files into (default: current working directory)
- `--size` (string): media size: fullsize, preview, or thumbnail (see the AssetMediaSize spec enum; 'original' is not accepted here — the spec deprecates size=original on this endpoint, use 'assets download-original' instead) [default: "preview"]
- `--edited` (bool): return the edited version of the asset if available
- `--quiet` (bool): disable per-file progress bars on stderr

### assets upload

Upload one or more files as new assets (POST /assets)
Args: `[FILE ...]`
- `--file-created-at` (string): override file creation date (RFC3339, default: file mtime)
- `--file-modified-at` (string): override file modification date (RFC3339, default: file mtime)
- `--filename` (string): override the uploaded file name (single FILE only)
- `--duration` (int): duration in milliseconds (for videos) [default: 0]
- `--is-favorite` (string): mark as favorite: true or false
- `--visibility` (string): asset visibility: archive, hidden, locked, or timeline
- `--live-photo-video-id` (string): live photo video asset `UUID`
- `--sidecar` (string): sidecar file to upload alongside (single FILE only)
- `--key` (string): shared-link key (query param)
- `--slug` (string): shared-link slug (query param)
- `--checksum` (string): sha1 checksum for duplicate detection before upload (x-immich-checksum header)
- `--json` (bool): print results as a JSON array
- `--quiet` (bool): disable per-file progress bars on stderr (--json implies quiet)

### assets check-remote-exists (alias: check-bulk-upload)

Check if local files already exist on the server via checksum (POST /assets/bulk-upload-check)
Args: `[FILE|DIR ...]`
- `--json` (bool): print results as a JSON array
- `--duplicates-only` (bool): show only files already on the server
- `--missing-only` (bool): show only files missing on the server
- `--ids-only, -q` (bool): print only duplicate asset IDs, one per line (pipeable into albums add-assets / tags tag)

### assets delete

Delete one or more assets (DELETE /assets)
Args: `[ASSET_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--force` (bool): permanently delete instead of moving to trash
- `--dry-run` (bool): print the assets that would be deleted without changing anything
- `--yes` (bool): skip the confirmation prompt before deleting assets

### assets copy

Copy metadata from one asset to another (PUT /assets/copy)
Args: `SOURCE_ID TARGET_ID`
- `--albums` (string): copy album associations: true or false
- `--favorite` (string): copy favorite status: true or false
- `--shared-links` (string): copy shared links: true or false
- `--sidecar` (string): copy sidecar file: true or false
- `--stack` (string): copy stack association: true or false

## albums

Album operations

### albums list

List albums (GET /albums)
- `--asset-id` (string): filter albums containing this asset `ID` (ignores other filters)
- `--id` (string): filter by album `ID`
- `--name` (string): filter by album name (exact match)
- `--owned` (string): filter by ownership: true = only owned, false = only shared-with-me
- `--shared` (string): filter by shared status: true = only shared, false = not shared
- `--json` (bool): print the raw response as a JSON array

### albums get

Show one or more albums by ID, including members (GET /albums/{id})
Args: `[ALBUM_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--json` (bool): print the raw responses as a JSON array

### albums assets

List every asset in one album (POST /search/metadata)
- `--album-id` (string): album `UUID` (mutually exclusive with --album-name)
- `--album-name` (string): album name, exact match (mutually exclusive with --album-id)
- `--json` (bool): print the raw responses as a JSON array
- `--ids-only, -q` (bool): print only asset IDs, one per line (useful for piping to other commands)
- `--yes` (bool): auto-accept a whitespace-variant album name match without prompting

### albums add-users

Share an album with a user (PUT /albums/{id}/users)
Args: `ALBUM_ID`
- `--user` (string): target user: exact user `UUID`, or a case-insensitive substring of their name/email [required]
- `--role` (string): album role to grant: editor, viewer, or owner [default: "viewer"]
- `--dry-run` (bool): print the planned share without changing anything

### albums rename

Rename an album (PATCH /albums/{id})
- `--name` (string): new album `NAME` [required]
- `--album-id` (string): target album `UUID` (mutually exclusive with --album-name)
- `--album-name` (string): target album's current name, exact match (mutually exclusive with --album-id)
- `--dry-run` (bool): print the planned rename without changing anything

### albums add-assets

Add assets to an album (PUT /albums/{id}/assets)
Args: `ALBUM_ID [ASSET_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--dry-run` (bool): print the assets that would be added without changing anything
- `--yes` (bool): skip the confirmation prompt before adding assets

### albums remove-assets

Remove assets from an album (DELETE /albums/{id}/assets)
Args: `ALBUM_ID [ASSET_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--dry-run` (bool): print the assets that would be removed without changing anything
- `--yes` (bool): skip the confirmation prompt before removing assets

### albums create

Create an album (POST /albums)
- `--name` (string): new album `NAME` [required]
- `--description` (string): album `DESCRIPTION`

### albums delete

Delete one or more albums by ID (DELETE /albums/{id})
Args: `[ALBUM_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--force` (bool): also delete albums that still contain assets (otherwise non-empty albums are refused)
- `--dry-run` (bool): print the albums that would be deleted without changing anything
- `--yes` (bool): skip the confirmation prompt before deleting albums

## search

Search operations

### search metadata

Search assets by metadata criteria (POST /search/metadata)
- `--original-file-name, -n` (string): filter by original file name (substring match)
- `--original-path` (string): filter by original file path
- `--type` (string): filter by asset type: IMAGE, VIDEO, AUDIO, OTHER
- `--is-favorite` (string): filter favorites: true or false
- `--is-not-in-album` (string): filter assets not in any album: true or false
- `--is-offline` (string): filter offline assets: true or false
- `--is-motion` (string): filter motion photos: true or false
- `--city` (string): filter by city name
- `--country` (string): filter by country name
- `--state` (string): filter by state/province name
- `--make` (string): filter by camera make
- `--model` (string): filter by camera model
- `--lens-model` (string): filter by lens model
- `--taken-after` (string): filter by taken date after (RFC3339, e.g. 2024-01-01T00:00:00Z)
- `--taken-before` (string): filter by taken date before (RFC3339, e.g. 2024-12-31T23:59:59Z)
- `--ocr` (string): filter by OCR text content
- `--description` (string): filter by description text
- `--order` (string): sort order: asc or desc (default: desc) [default: "desc"]
- `--page-size` (int): number of results per page (max 1000) [default: 100]
- `--all` (bool): fetch all pages automatically (overrides --page-size limit)
- `--json` (bool): print raw JSON response
- `--ids-only, -q` (bool): print only asset IDs, one per line (useful for piping to other commands)

## tags

Tag operations

### tags list

List all tags (GET /tags)
- `--json` (bool): print the raw response as a JSON array

### tags get

Show one or more tags by ID (GET /tags/{id})
Args: `[TAG_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--json` (bool): print the raw responses as a JSON array

### tags delete

Delete one or more tags by ID (DELETE /tags/{id})
Args: `[TAG_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--dry-run` (bool): print the tags that would be deleted without changing anything
- `--yes` (bool): skip the confirmation prompt before deleting tags

### tags upsert

Create tags (and missing parents) by full path, or return existing ones (PUT /tags)
Args: `[TAG_VALUE ...]`
- `--json` (bool): print the raw response as a JSON array

### tags bulk-tag

Add multiple tags to multiple assets in one request (PUT /tags/assets)
Args: `[ASSET_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--tag-id` (string): tag `ID` to add (repeatable)
- `--dry-run` (bool): print what would be tagged without changing anything

### tags tag

Add one tag to multiple assets (PUT /tags/{id}/assets)
Args: `TAG_ID [ASSET_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--dry-run` (bool): print what would be tagged without changing anything

## users

User operations

### users me

Show the user that owns the API key (GET /users/me)
- `--json` (bool): print the raw response as JSON

### users list

List all users on the server (GET /users)
- `--json` (bool): print the raw response as a JSON array

### users get

Show one or more users by ID (GET /users/{id})
Args: `[USER_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--json` (bool): print the raw responses as a JSON array

## client-workflow

Client-side multi-step workflows

### client-workflow replace-asset

Replace an existing asset with a new file, keeping its metadata
Args: `[ASSET_ID NEW_FILE_PATH]`
- `--replace-file` (string): read asset-id;new-file-path pairs from `FILE`, one per line ('-' for stdin; '#' or '//' starts a comment)
- `--dont-remove-original-file` (bool): keep the original asset instead of removing it after a successful replacement
- `--force` (bool): permanently delete the original asset instead of moving it to trash
- `--dry-run` (bool): print the planned steps for each pair without changing anything
- `--yes` (bool): skip the confirmation prompt before removing original assets

### client-workflow tag-delete

Delete tags whose full path matches an include/exclude regex
- `--include` (string): only delete tags whose full path matches this `REGEX` (default: match all)
- `--exclude` (string): never delete tags whose full path matches this `REGEX`
- `--dry-run` (bool): print the tags that would be deleted without changing anything
- `--yes` (bool): skip the confirmation prompt before deleting tags

### client-workflow add-users-to-album-with-pattern

Share every album whose name matches an include/exclude regex with a user
- `--include` (string): only share albums whose name matches this `REGEX` (default: match all)
- `--exclude` (string): never share albums whose name matches this `REGEX`
- `--user` (string): target user: exact user `UUID`, or a case-insensitive substring of their name/email [required]
- `--role` (string): album role to grant: editor, viewer, or owner [default: "viewer"]
- `--dry-run` (bool): print the albums that would be shared without changing anything
- `--interactive, -i` (bool): ask once per album (y/N) instead of one bulk confirmation; --yes overrides this and skips all prompts
- `--yes` (bool): skip the confirmation prompt(s) before sharing albums

### client-workflow find-no-thumbhash

Find assets that have no thumbhash (likely corrupt or unprocessed)
- `--original-file-name, -n` (string): pre-filter by original file name (substring match)
- `--type` (string): pre-filter by asset type: IMAGE, VIDEO, AUDIO, OTHER
- `--album-id` (string): restrict the scan to assets in this album `UUID`
- `--page-size` (int): number of assets per API page (max 1000) [default: 250]
- `--json` (bool): print results as JSON array
- `--ids-only, -q` (bool): print only asset IDs, one per line

### client-workflow find-heic-tile-defect

Find HEIC/HEIF assets whose dimensions trigger a known Immich thumbnail-corruption defect
- `--original-file-name, -n` (string): pre-filter by original file name (substring match)
- `--album-id` (string): restrict the scan to assets in this album `UUID`
- `--page-size` (int): number of assets per API page (max 1000) [default: 250]
- `--tile-size` (int): assumed HEIF grid tile size in pixels [default: 512]
- `--json` (bool): print results as JSON array
- `--ids-only, -q` (bool): print only asset IDs, one per line
- `--apply-tag` (bool): tag every found candidate asset with --tag (default: report only, no tagging)
- `--tag` (string): tag value (full path) applied to found candidates when --apply-tag is set [default: "immich-admin-cli/corrupt-heic"]
- `--dry-run` (bool): with --apply-tag, print what would be tagged without changing anything

### client-workflow repair-assets

Repair corrupt image assets and re-import them, keeping metadata
Args: `[ASSET_ID ...]`
- `--mode` (string): repair mode: all | marker | tiff-tags | takeout-json [default: "all"]
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--check-all-assets` (bool): attempt repair on every IMAGE asset with no thumbhash (cannot be combined with explicit IDs)
- `--album-id` (string): attempt repair on every IMAGE asset with no thumbhash in this album `UUID` (cannot be combined with explicit IDs or --check-all-assets)
- `--keep-original` (bool): repair and re-import but keep the original asset instead of removing it
- `--force` (bool): permanently delete the original asset instead of moving it to trash
- `--page-size` (int): number of assets per API page when scanning with --check-all-assets (max 1000) [default: 250]
- `--dry-run` (bool): print the planned steps for each asset without changing anything
- `--yes` (bool): skip the confirmation prompt before removing original assets

### client-workflow fix-album-dates

Check assets in date-named albums against the date implied by the album name, and offer to fix mismatches
- `--dry-run` (bool): print the planned date fixes without changing anything
- `--offset-days` (int): allow assets up to this many days before/after the album's date before flagging them (absorbs camera timezone/DST boundary slack) [default: 2]
- `--interactive, -i` (bool): ask once per album (y/N) instead of one bulk confirmation; --yes overrides this and skips all prompts
- `--yes` (bool): skip the confirmation prompt(s) before fixing dates

### client-workflow download-album

Download all original files or a smaller variant (preview/thumbnail/fullsize) from one album into a local folder, optionally kept in sync
- `--album-id` (string): album `ID` to download (mutually exclusive with --album-name)
- `--album-name` (string): album name to download; must resolve to exactly one album (mutually exclusive with --album-id)
- `--target-dir` (string): local directory to download into (created if missing) [required]
- `--size` (string): media variant to download: original, fullsize, preview, or thumbnail (see the AssetMediaSize spec enum) [default: "preview"]
- `--ignore-videos` (bool): skip video assets
- `--sync` (bool): keep --target-dir in sync using a manifest: skip unchanged assets, re-download changed ones, and delete local files for assets removed from the album
- `--timestamp-prefix` (bool): prefix each local file name with the asset's capture date/time ("yyyy-MM-dd_HH_mm_ss", from its metadata)
- `--resize` (bool): resize/re-encode every downloaded file to JPEG using ImageMagick (path from config tools.imagemagick_path or IMMICH_IMAGEMAGICK_PATH, falling back to PATH)
- `--resize-width` (int): target width in pixels; 0 = unconstrained on that axis (requires --resize) [default: 0]
- `--resize-height` (int): target height in pixels; 0 = unconstrained on that axis (requires --resize) [default: 0]
- `--resize-quality` (int): JPEG quality 1-100 (requires --resize) [default: 85]
- `--resize-video-preset` (string): re-encode every downloaded VIDEO asset using ffmpeg (path from config tools.ffmpeg_path or IMMICH_FFMPEG_PATH, falling back to PATH); videos are always fetched at --size original for this regardless of --size (many videos have no usable preview/thumbnail rendition, and ffmpeg needs the real stream anyway) — only non-video assets use --size; valid presets: 1080p-web-friendly
- `--dry-run` (bool): print the planned downloads/deletions without changing anything
- `--yes` (bool): skip the confirmation prompt before deleting local files (--sync only)
- `--quiet` (bool): disable per-file progress bars on stderr

### client-workflow merge-album

Move every asset from one album into another, optionally deleting the emptied source
- `--from` (string): source album `UUID` (assets move out of it) [required]
- `--into` (string): target album `UUID` (assets move into it) [required]
- `--delete-empty-source` (bool): delete the source album when it holds no assets after the move
- `--dry-run` (bool): print the merge plan without changing anything
- `--yes` (bool): skip the confirmation prompt before moving assets

### client-workflow watch-upload

Watch a folder and auto-upload new stable files (POST /assets/bulk-upload-check + POST /assets)
- `--watch-dir` (string): local directory to watch [required]
- `--mode` (string): flat or by-subfolder [default: "flat"]
- `--tag-pattern` (string): tag pattern for --mode flat ({yyyy-MM-dd} = upload day) [default: "immich-admin-cli/watch/{yyyy-MM-dd}"]
- `--depth` (int): subfolder depth (v1: only 1 supported) [default: 1]
- `--album-id` (string): opt-in album `ID` to also add uploads to
- `--album-name` (string): opt-in album name to also add uploads to (created unless --dry-run)
- `--interval` (string): poll interval (e.g. 60s); <=0 means run once [default: "60s"]
- `--stable-for` (string): defer files changed within this long (e.g. 30s) [default: "30s"]
- `--once` (bool): run a single scan and exit (for cron)
- `--dry-run` (bool): print what would be uploaded/linked without changing anything
- `--yes` (bool): skip creation prompts for tags/albums
- `--quiet` (bool): disable per-file progress bars on stderr (--json implies quiet)
- `--json` (bool): print per-interval stats as JSON on stdout

### client-workflow watch-download

Keep a local folder in sync from an album or tag (loop around download-album --sync)
- `--album-id` (string): album `ID` source (mutually exclusive with --album-name/--tag-*)
- `--album-name` (string): album name source
- `--tag-id` (string): tag `ID` source (mutually exclusive with --tag-value/album-*)
- `--tag-value` (string): tag value (full path) source
- `--target-dir` (string): local directory to sync into (created if missing) [required]
- `--size` (string): media variant: original, fullsize, preview, or thumbnail [default: "original"]
- `--interval` (string): poll interval (e.g. 300s); <=0 means run once [default: "300s"]
- `--once` (bool): run a single sync and exit (for cron)
- `--dry-run` (bool): print the planned sync without changing anything
- `--yes` (bool): skip the deletion confirmation prompt
- `--quiet` (bool): disable per-file progress bars on stderr (--json implies quiet)
- `--json` (bool): print per-interval stats as JSON on stdout

## immich-workflow

Server-side workflow operations

### immich-workflow list

List server workflows (GET /workflows)
- `--description` (string): filter by workflow description
- `--enabled` (string): filter by enabled status: true or false
- `--id` (string): filter by workflow `ID`
- `--logging` (string): filter by whether the workflow logs run results: true or false
- `--name` (string): filter by workflow name
- `--trigger` (string): filter by trigger type: AssetCreate, AssetMetadataExtraction, or AssetTagged
- `--json` (bool): print the raw response as a JSON array

### immich-workflow get

Show one or more server workflows by ID (GET /workflows/{id})
Args: `[WORKFLOW_ID ...]`
- `--ids-file` (string): read IDs from `FILE`, one UUID per line ('-' for stdin; '#' or '//' starts a comment)
- `--json` (bool): print the raw responses as a JSON array

### immich-workflow logs

Show run logs for one server workflow (GET /workflows/{id}/logs)
Args: `WORKFLOW_ID`
- `--before` (string): only show runs before this date/time (RFC3339)
- `--limit` (int): maximum number of log entries (default: server default, 50) [default: 0]
- `--result` (string): only show runs with this result: completed, halted, or error
- `--json` (bool): print the raw response as a JSON array

### update

Check for and install the latest release from GitHub
- `--check` (bool): only check for a new version, don't install it
- `--yes` (bool): install without a confirmation prompt


## Best practices

### Prerequisites

- The `immich-admin` binary and network access to an Immich server **v3.2.0
  or newer** (every command prints `account (server, server X.Y.Z, needs >=
  3.2.0)` to stderr and warns on older servers).
- Config via `--config FILE` (default `config.prod.yaml`);
  `IMMICH_SERVER` / `IMMICH_API_KEY` env vars override file values.
- This skill's command catalog below is generated from the CLI itself; run
  `immich-admin <group> <command> --help` for full flags and examples.

### Safety discipline

- Every command prints the account + server identity to stderr — always
  check it before acting.
- Preview destructive work with `--dry-run` first, confirm with `--yes`
  only in automation.
- Deletions go to **trash** (restorable) by default; `--force` deletes
  permanently. Tag deletion is the exception: tags have no trash and
  deleting a parent deletes its children — always dry-run first.

### ID plumbing (the core pattern)

- `--ids-only` (short `-q`) prints one UUID per line — pipe it into the
  next command or save it: `search metadata --all -q > ids.txt`.
- `--ids-file FILE` reads IDs back (one UUID per line, `#`/`//` comments
  and blank lines skipped, `-` means stdin). Positional IDs and
  `--ids-file` combine.
- Bulk commands continue on per-ID errors and exit non-zero with a
  `N of M failed` summary — inspect stderr, don't assume all-or-nothing.

### Prefer --json for complete information

Human output is lossy by design (one-line summaries plus a count). Almost
every read command also accepts `--json`, which prints the raw API
response with all fields. Whenever you need the full record — for
analysis, filtering, or feeding another tool — prefer it:

```sh
immich-admin immich-workflow list --json
immich-admin assets info <ASSET_ID> --json
immich-admin search metadata --original-file-name IMG --all --json
```

### From server errors to asset IDs

Extract failing asset UUIDs from the Immich server log and feed them to
the CLI (PowerShell):

```powershell
ssh SSH_CONFIG_NAME 'docker logs --tail 2000 immich_server 2>&1 | grep ERROR' |
  ForEach-Object {
    [pscustomobject]@{
      id   = if ($_ -match '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}') { $Matches[0] } else { $null }
      line = $_
    }
  } | ConvertTo-Json
```

Keep the matched IDs, inspect, then act:

```powershell
# ids.txt: one UUID per line, e.g. from ($parsed | Where-Object id).id
immich-admin assets info --ids-file ids.txt
```

### Before merge-album, sanity-check the pair

Human output is lossy — always inspect both albums with `--json` first:

```sh
immich-admin albums get <FROM> --json
immich-admin albums get <INTO> --json
```

Compare `startDate`/`endDate` (album time range) and scan `albumName`/
`description` of both for year numbers (e.g. `\b(19|20)\d{2}\b`). For the
per-asset spread, use `albums assets --album-id <ID> --json`
(`localDateTime`/`fileCreatedAt`). If the years or time ranges don't
overlap (e.g. source is "2010 USA", target is "2024 Garten"), the merge is
probably unwanted — stop and ask the user whether to merge, keep the
albums separate, or rename instead of merging blindly. Always run
`client-workflow merge-album --dry-run` first.

### Canonical flows

- **Corrupt hunt**: `client-workflow find-no-thumbhash --type IMAGE -q >
  corrupt.txt` (assets Immich couldn't thumbnail), then `client-workflow
  repair-assets --mode all --ids-file corrupt.txt --dry-run` — `marker`
  fixes JPEGs missing the EOI marker, `tiff-tags` patches zero-count TIFF
  IFD entries, `takeout-json` deletes unrecoverable Google Takeout JSON
  sidecars (trash by default). Re-check with `find-no-thumbhash` after.
- **Search → act**: `search metadata` filters by name, type, favorite,
  album membership, location, camera, dates, OCR (`--all` pages the whole
  library). Useful sets: `--is-not-in-album true` (orphans),
  `--is-offline true`, `--make/--model`, `--city/--taken-after`.
- **Replace**: `client-workflow replace-asset <ASSET_ID> <NEW_FILE>`
  uploads, checksum-verifies, copies metadata (albums, favorite, shared
  links, sidecar, stack), then trashes the original. Aborts on checksum
  duplicates instead of touching the wrong asset.
- **Albums**: `albums create --name [--description]`; `albums assets --album-id/--album-name [--json] [-q]` lists every asset (pipe `-q` into `add/remove-assets --ids-file -`); `albums add-assets /
  remove-assets ALBUM_ID [--ids-file] [--dry-run] [--yes]` (reports
  added-or-removed / already-present / not-found per ID); `albums delete
  [--force]` (refuses non-empty albums without `--force`; the server trashes
  and finishes deletion in a background job, so a deleted album can linger
  briefly). `client-workflow merge-album --from --into
  [--delete-empty-source] [--dry-run] [--yes]` moves every asset out of one
  album into another for duplicate cleanup. `client-workflow download-album --album-name NAME --target-dir
  DIR --size original --sync` mirrors an album (manifest-tracked, safe to
  resume); `--size preview` + `--resize`/`--resize-video-preset` for small
  shareable copies. `fix-album-dates` reconciles date-named albums
  (report-only for year albums). `add-users-to-album-with-pattern` bulk-shares.
- **Tags**: `tags upsert`, `tags bulk-tag`, `tags tag` for assignment;
  `client-workflow tag-delete --include/--exclude` for regex bulk deletion
  (permanent — dry-run first).
- **Upload**: `assets upload FILE...` (timestamps default to file mtime;
  `--sidecar`/`--filename` are single-file only; per-file byte bar on stderr, `--quiet` disables, `--json` implies quiet).
  `assets check-remote-exists FILE|DIR...` (alias `check-bulk-upload`) hashes locally and reports
  `uploaded <id>` / `missing` / `unsupported` without mutating (`--json`, `--ids-only -q` pipes duplicate IDs into `albums add-assets`/`tags tag`).
- **Watch**: `client-workflow watch-upload --watch-dir DIR --mode flat|by-subfolder [--tag-pattern "immich-admin-cli/watch/{yyyy-MM-dd}"] [--album-id|--album-name] --interval 60s --stable-for 30s [--once] [--dry-run] [--yes]`
  polls and uploads only stable files (bulk-check first; duplicates only linked). `client-workflow watch-download (--album-id|--album-name|--tag-id|--tag-value) --target-dir DIR --size original --interval 300s [--once]`
  loops the `.immich-sync.json` manifest sync (album or tag source). `--interval <=0` means run once. Per-interval one-line summary on stderr; `--json` prints JSON stats on stdout.
