# immich-admin-cli

[![Build](https://img.shields.io/github/actions/workflow/status/dhcgn/immich-admin-cli/ci.yml?branch=main)](https://github.com/dhcgn/immich-admin-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/dhcgn/immich-admin-cli)](https://github.com/dhcgn/immich-admin-cli/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/dhcgn/immich-admin-cli)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/dhcgn/immich-admin-cli)](https://goreportcard.com/report/github.com/dhcgn/immich-admin-cli)
[![Downloads](https://img.shields.io/github/downloads/dhcgn/immich-admin-cli/total)](https://github.com/dhcgn/immich-admin-cli/releases)
[![License](https://img.shields.io/github/license/dhcgn/immich-admin-cli)](LICENSE)

> [!WARNING]
> **No warranty. Use at your own risk.**
> This tool performs bulk and destructive operations (deleting, replacing, and re-encoding assets) directly against your Immich server. A bug, a wrong flag, or an unexpected server response **can cause permanent data loss**. Always have a verified backup, test against non-critical data first, and read what a command will do (`--dry-run`) before running it. This project is not affiliated with the Immich project.

## Motivation

I want to be able to manage my Immich server from the command line for my large photo collection.
There are some bulk features that are not available in the web interface, and I want to be able to automate some tasks. Like compression, converting, repairing, and deleting photos.

## Self-Update

`immich-admin update` checks the latest [GitHub release](https://github.com/dhcgn/immich-admin-cli/releases) for a build matching the current OS/arch and, on confirmation, replaces the running executable in place and restarts it (via [`gh-update`](https://github.com/dhcgn/gh-update)).

```sh
immich-admin update            # check, prompt, then install
immich-admin update --check    # only check, don't install
immich-admin update --yes      # install without prompting
```

Release binaries are named `immich-admin_<os>_<arch>[.exe]` — deliberately **without** a version number, since `update` overwrites the file in place while keeping its original name; a version baked into the filename would go stale as soon as it self-updates. Use `immich-admin --version` or the release tag to see what's actually installed.

## Client Workflows

The main purpose of this tool: **client workflows** are multi-step orchestrations that combine several Immich API calls with local processing (e.g. re-encoding) into one command. They run entirely on the client — not to be confused with Immich's server-side Workflows API.

All workflows follow the same safety model: `--dry-run` shows what would happen without changing anything, the destructive final step requires `--yes`, originals are only removed **after** the replacement has been uploaded and verified, and removal goes to the trash (restorable) by default.

| Status | Command | Description |
|:------:|---------|-------------|
| ✅ done | `client-workflow replace-asset` | Replace an existing asset with a new file, keeping its metadata |
| ✅ done | `client-workflow tag-delete` | Delete tags whose full path matches an include/exclude regex |
| ✅ done | `client-workflow add-users-to-album-with-pattern` | Share every album whose name matches an include/exclude regex with a user |
| ✅ done | `client-workflow find-no-thumbhash` | Find assets without a thumbhash (likely corrupt or unprocessed) |
| ✅ done | `client-workflow repair-assets` | Repair corrupt JPEG (missing EOI marker) and TIFF (invalid zero-count IFD tag) assets and re-import them, keeping metadata |
| ✅ done | `client-workflow fix-album-dates` | Check assets in date-named albums ("2025-07-04 Garten", "2010 USA") against the date implied by the album name, and offer to fix mismatches |
| ✅ done | `client-workflow download-album` | Download all originals or all thumbnails from one album to a local folder, optionally excluding videos, optionally kept in sync |
| ⏳ planned | `client-workflow reencode-jxl` | Re-encode assets to JPEG XL (`cjxl`), then replace the originals |
| ⏳ planned | `client-workflow reencode-jpegli` | Re-encode assets with jpegli (`cjpegli`), then replace the originals |

### `client-workflow replace-asset` (alias `cw`)

The reusable core of the re-encode workflows:

1. **Upload** the new file as a new asset
2. **Verify** the upload (asset exists, checksum matches the local file)
3. **Copy metadata** from the old asset — albums, favorite, shared links, sidecar, stack association (`PUT /assets/copy`)
4. **Remove** the old asset (to trash by default; `--force` for permanent deletion, `DELETE /assets`)

```sh
# Single asset
immich-admin client-workflow replace-asset <ASSET_ID> <NEW_FILE_PATH>

# Bulk, from a file of "assetId;newFilePath" lines ('-' for stdin, '#' for comments)
immich-admin client-workflow replace-asset --replace-file pairs.txt
```

Flags: `--replace-file FILE`, `--dont-remove-original-file` (keep the original instead of removing it), `--force` (permanently delete instead of trashing), `--dry-run`, `--yes` (skip the removal confirmation prompt).

If the uploaded file's checksum matches an existing asset (Immich reports it as a duplicate instead of creating a new one), the workflow aborts that asset rather than risk acting on the wrong asset.

### `client-workflow reencode-jxl`

1. **Download** the original asset
2. **Encode** to JPEG XL with `cjxl` — lossless JPEG transcode where the source is a JPEG, preserving EXIF
3. **Replace** the original via the replace-asset steps above

### `client-workflow reencode-jpegli`

1. **Download** the original asset
2. **Re-encode** with `cjpegli` — noticeably smaller JPEGs that remain compatible with everything
3. **Replace** the original via the replace-asset steps above

**Requirements:** the re-encode workflows need the external encoders (`cjxl`, `cjpegli`) available on `PATH` or configured in the config file.

### `client-workflow tag-delete` (alias `cw`)

Bulk-delete tags selected by regex:

1. **Fetch** all tags (`GET /tags`)
2. **Filter** by `--include` / `--exclude` regexes, matched against each tag's **full path** (`Value`, e.g. `Travel/2024`)
3. **Preview** — print every tag that would be deleted
4. **Delete** the matched tags (`DELETE /tags/{id}`)

```sh
# Delete every tag EXCEPT those whose path contains "immich-go"
immich-admin client-workflow tag-delete --exclude "immich-go" --dry-run
```

Flags: `--include REGEX` (default: match all), `--exclude REGEX`, `--dry-run`, `--yes` (skip the confirmation prompt). Filters are passed as flags only — stray positional arguments are rejected so a typo like `exclude` (instead of `--exclude`) fails loudly instead of matching everything.

> ⚠️ Unlike the asset workflows, tag deletion is **permanent** — the Tags API has no trash. Deleting a parent tag also deletes its child tags server-side. Always run with `--dry-run` first.

### `client-workflow add-users-to-album-with-pattern` (alias `cw`)

Bulk-share albums selected by regex with one user:

1. **Resolve** `--user` to exactly one person — an exact UUID, or a case-insensitive substring match against every user's name/email (`GET /users`); errors out and lists the candidates if the query is ambiguous or matches nobody
2. **Fetch** all albums (`GET /albums`)
3. **Filter** by `--include` / `--exclude` regexes, matched against each album's name
4. **Preview** — print every matched album (ID, name, asset count), flagging any where the user already has access
5. **Confirm** — either one bulk yes/no prompt for all matched albums, or, with `--interactive`, one yes/no prompt **per album** so you can pick and choose
6. **Share** the confirmed albums with that user at `--role` (`PUT /albums/{id}/users`); albums where the user already has access are skipped without asking

```sh
# Share every album whose name contains "Amy" or "Amelia" with Julia, as viewer
immich-admin client-workflow add-users-to-album-with-pattern --include "Amy|Amelia" --user Julia --dry-run

# Same, but decide album-by-album instead of one bulk yes/no
immich-admin client-workflow add-users-to-album-with-pattern --include "Amy|Amelia" --user Julia --interactive
```

Flags: `--include REGEX` (default: match all), `--exclude REGEX`, `--user STRING` (required; UUID or name/email substring), `--role editor|viewer|owner` (default `viewer`), `--interactive`/`-i` (ask once per album instead of one bulk confirmation), `--dry-run`, `--yes` (skip all prompts — bulk or interactive).

### `client-workflow find-no-thumbhash`

Finds assets whose `thumbhash` field is null or empty — a reliable indicator that Immich could not generate a thumbnail, which usually means the file is **corrupt or unprocessable**.

This is a non-destructive, **read-only** workflow that scans all assets via `POST /search/metadata` with automatic pagination, checking each asset's thumbhash in the response. No per-asset API call is needed.

```sh
# Find all assets without thumbhash
immich-admin client-workflow find-no-thumbhash

# Only images, output as one ID per line (for piping)
immich-admin cw find-no-thumbhash --type IMAGE --ids-only

# Pre-filter by file name, JSON output
immich-admin cw find-no-thumbhash --original-file-name JPG --json
```

Flags: `--type TYPE` (pre-filter: IMAGE, VIDEO, …), `--original-file-name NAME` (substring match), `--album-id UUID` (restrict the scan to one album), `--page-size N` (default 250, max 1000), `--json`, `--ids-only` / `-q`.

Typical follow-up — save corrupt IDs, then use them later with a repair workflow:

```sh
# Save corrupt asset IDs to a file
immich-admin cw find-no-thumbhash --type IMAGE -q > corrupt-ids.txt

# Inspect each one
cat corrupt-ids.txt | ForEach-Object { immich-admin assets info $_ }
```

### `client-workflow repair-assets` (alias `cw`)

Repairs corrupt JPEG and TIFF assets and re-imports each fixed file, keeping all metadata. Two independent, precisely-detected defects are covered, plus a separate **delete** mode for an unrecoverable Google Takeout corruption:

- **JPEG — missing End-of-Image marker** (`FF D9`): the image data is fully intact, only the mandatory two trailing bytes are gone, so Immich can't generate a thumbnail (no thumbhash).
- **TIFF — invalid zero-count IFD tag**: some cameras/scanners write a private tag (e.g. `0x8657`/`0x8658`) with its 4-byte *count* field set to `0`, which the TIFF spec never allows. libtiff (which Immich's thumbnailer uses) fatally rejects this as `Input file has corrupt header: ... Null count for "Tag N"`, even though the actual image data is fine — ffmpeg-based tools ignore the defect, so it only surfaces as an Immich server-side thumbnail failure.
- **Google Takeout JSON sidecar imported as the photo** (`takeout-json` mode — *delete*, not repair): a known Google Photos Takeout export/import failure where a photo's `*.json` metadata sidecar is stored under the real photo's name. The file contains **only JSON, no image data**, so the header/pixels needed to reconstruct the picture are physically gone — it is **not** recoverable by any tool. The only safe action is to remove the junk asset.

Repair modes (extensible — new modes can be added without changing the command):

| Mode | What it does | Loss |
|------|--------------|------|
| `marker` | Appends the missing `FF D9` End-of-Image marker to JPEGs (append-only) | None — EXIF preserved |
| `tiff-tags` | Patches each IFD entry with an invalid zero count field to `1`, in place | None — pixel data, EXIF/XMP/dates all untouched |
| `takeout-json` | **Deletes** assets that are actually a Google Takeout JSON sidecar (no image data, unrecoverable). Trash by default; `--force` to delete permanently. Opt-in only — **not** part of `all` | Deletes the junk asset (nothing recoverable to lose) |
| `all` | Runs every safe **repair** strategy across both file types (currently `marker` + `tiff-tags`). Excludes the destructive `takeout-json` mode | None |

> 🎯 **Detection is structural, not a blind per-extension guess.** `tiff-tags` only ever applies when a raw IFD-chain walk (byte-level, no image decode — so it works regardless of compression/bit-depth) both (a) completes cleanly and (b) finds at least one entry whose count field is literally `0` — the exact, unambiguous condition libtiff rejects. A TIFF without that specific defect is reported as **already-ok** and left untouched; a TIFF whose IFD chain can't be parsed at all is reported **unrepairable**, never guessed at. The same principle applies to `marker`: it only fires when the byte-level SOI/EOI check finds the exact missing-EOI pattern. `--mode all` therefore never touches a file for a reason it can't concretely point to.

> 🗑️ **`takeout-json` deletes, so its detection is deliberately the strictest.** A file is only ever deleted when its leading bytes parse as a complete JSON object **and** carry the full Google Takeout fingerprint — `title` **and** `photoTakenTime.timestamp` **and** `creationTime.timestamp` **and** `googlePhotosOrigin` must all be present. A real image never parses as a JSON object at all, and even an unrelated JSON file is extremely unlikely to carry that exact combination, so a real photo can never be mistaken for a sidecar. Anything that doesn't match is reported **skipped-not-sidecar** and left completely untouched. Use `--dry-run` to list what would be deleted first, and `--force` only if you want permanent deletion instead of trash. `--keep-original` is not applicable to this mode (there is nothing to keep).

> 💡 **The repair modes point you at `takeout-json` automatically.** When `marker`, `tiff-tags` or `all` encounter an asset whose bytes are actually a Google Takeout sidecar (rather than a repairable image), they don't just report it as unrepairable — they print a per-asset hint like `… is a Google Takeout JSON sidecar (original title "IMG_1366.jpg"), not a repairable image — rerun with --mode takeout-json to delete it`. This works for supported types (already downloaded for repair) and for other extensions (e.g. a `.dng` that is really a sidecar), where only the file's head is fetched to recognize it — no full download.

Because the repaired bytes differ, the fix is a **re-import** built on the [`replace-asset`](#client-workflow-replace-asset-alias-cw) flow:

1. **Download** the original asset's bytes
2. **Detect** the problem (JPEG: byte-level SOI/EOI marker check; TIFF: IFD-chain walk for zero-count entries) and **repair** locally
3. **Upload** the repaired file as a new asset and **verify** its checksum
4. **Copy metadata** from the original (albums, favorite, shared links, sidecar, stack)
5. **Remove** the original (trash by default; `--force` to permanently delete)

> ⚠️ **Removal is not gated on Immich generating a thumbnail.** A local decode is *not* used as the correctness check either: Go's `image/jpeg` is far stricter than Immich's (libjpeg/libvips) and rejects files Immich accepts — tested against 193 real corrupt files, Go decoded 0 of them even after a valid marker repair. An earlier version of this tool waited for Immich to generate a thumbhash for the new asset before removing the original, but server-side thumbnail generation is asynchronous and its timing depends on too many factors (job queue depth, server load, scheduling) to reliably bound with a fixed timeout — so that wait was removed. The original is now removed as soon as the upload is checksum-verified and metadata is copied. If an earlier step (upload or checksum verify or metadata copy) fails, the original is left untouched and the failed upload is rolled back (trashed). **Use [`find-no-thumbhash`](#client-workflow-find-no-thumbhash) afterwards** to confirm a repair actually produced a thumbnail, or pass `--keep-original` to be able to re-check before the original is gone.

```sh
# Repair specific assets by ID (dry-run first to preview the steps)
immich-admin client-workflow repair-assets --mode marker --dry-run <ASSET_ID> ...

# Repair from a file of IDs (e.g. produced by find-no-thumbhash)
immich-admin cw repair-assets --mode marker --ids-file corrupt-ids.txt

# Repair the zero-count IFD tag defect in TIFFs only
immich-admin cw repair-assets --mode tiff-tags --check-all-assets --yes

# Scan and repair EVERY IMAGE asset with no thumbhash, in one pass (JPEGs and TIFFs)
immich-admin cw repair-assets --mode all --check-all-assets --yes

# Scan and repair only the corrupt images inside one album
immich-admin cw repair-assets --mode all --album-id <ALBUM_ID> --yes

# Delete assets that are actually Google Takeout JSON sidecars (preview first, then run)
immich-admin cw repair-assets --mode takeout-json --check-all-assets --dry-run
immich-admin cw repair-assets --mode takeout-json --check-all-assets --yes
```

The asset source is exactly one of: explicit IDs (positional and/or `--ids-file`), `--check-all-assets` (whole library), or `--album-id` (one album). With `--album-id` the album is validated first (`getAlbumInfo`) and its name/asset count are printed, then only that album's IMAGE assets with no thumbhash are scanned and repaired.

Flags: `--mode all|marker|tiff-tags|takeout-json` (default `all`), `--ids-file FILE`, `--check-all-assets` (scan all no-thumbhash IMAGE assets), `--album-id UUID` (scan only that album — mutually exclusive with explicit IDs and `--check-all-assets`), `--keep-original` (repair + re-import but leave the original untouched; not valid with `takeout-json`), `--force` (permanently delete instead of trashing), `--page-size N` (for `--check-all-assets` / `--album-id`, default 250), `--dry-run`, `--yes`. Assets with no applicable strategy for the chosen mode (e.g. non-JPEG/TIFF files, or unsupported RAW/DNG variants such as JPEG-XL-compressed DNGs) are skipped and reported. A per-asset outcome summary is printed at the end — `repaired / already-ok / skipped-unsupported / unrepairable` for the repair modes, or `deleted-sidecar / skipped-not-sidecar` for `takeout-json`.

### `client-workflow fix-album-dates`

Checks date plausibility for albums named after a date, and offers to fix stragglers:

1. **Fetch** all albums (`GET /albums`) and keep the ones whose name matches `yyyy-MM-dd <title>` (e.g. "2025-07-04 Garten") or `yyyy <title>` (e.g. "2010 USA")
2. **Fetch** each matched album's assets (`POST /search/metadata`, filtered by album)
3. **Compare** each asset's local capture date (`LocalDateTime`) against the range implied by the album name — that exact day, or the whole year
4. **Report** every mismatch found, worst album first (the album containing the single most out-of-range asset), each with a link to the album in the web UI
5. **Fix** — for exact-day albums only, offer to set the mismatched asset's date to the album's date, changing only the date and keeping the original time of day (`PUT /assets/{id}`)

Year-only albums (e.g. "2010 USA") are report-only: there's no single unambiguous date to reset an outlier to, so their mismatches are listed but never auto-fixed — this applies whether an asset falls outside the year itself or outside the `--offset-days`-widened boundary around it.

> 🕒 **Time zones**: the check uses `LocalDateTime`, Immich's own timezone-agnostic "photographer's local time" field (derived from EXIF) — not the UTC `fileCreatedAt` timestamp. The album name's date is parsed the same timezone-naive way, so no timezone conversion happens (or is needed) in the comparison itself. What timezones *can* still cause is a boundary slip: a camera whose clock/timezone or DST was set wrong around midnight can push `LocalDateTime` into the neighboring calendar day even though the photo was taken at the same real moment as the rest of the album. `--offset-days` (default `2`) exists specifically to absorb that: it widens the accepted range by that many days on each side before flagging a mismatch, without changing the date a fix would apply (always the album's exact nominal date).

> 💡 **Not every mismatch is a data-entry error.** Many albums are named after an anchor/start date but deliberately include photos from a wider range (multi-day trips, a themed album with a few older cover photos, etc.) — these are correctly detected as "outside the range" but fixing them would be wrong. Always review the printed report (or use `--dry-run`) before fixing, and prefer `--interactive` to accept or skip album-by-album rather than one bulk confirmation. `--interactive` shows and asks about one album at a time (instead of printing the whole report up front) so each prompt stays right below the details it refers to, without having to scroll back up. Each confirmed album is fixed immediately, right after you answer — not batched until the end — so pressing Ctrl+C at any point never loses an already-confirmed album; only albums not yet reached stay unfixed (just rerun the command to pick up where you left off). The bulk (non-interactive) confirmation instead warns plainly that it changes every listed asset automatically, with no per-album review.

```sh
# Preview issues across every date-named album
immich-admin client-workflow fix-album-dates --dry-run

# Use a narrower (or wider) tolerance than the default 2 days
immich-admin cw fix-album-dates --offset-days 1 --dry-run

# Review and fix album-by-album
immich-admin cw fix-album-dates --interactive

# Fix everything in one bulk confirmation
immich-admin cw fix-album-dates --yes
```

Sample report line (each flagged album is followed by a link to it in the web UI, using the configured `server`, and each fixable asset shows the date it would be changed to):

```
2025-07-04 Garten (d4856b75-7700-414c-9dc2-4f6d501936d1)  pattern=day  range=2025-07-04..2025-07-05 (±2 day(s))
https://immich.example.com/albums/d4856b75-7700-414c-9dc2-4f6d501936d1
  a1b2c3d4-...  IMG_1234.jpg  local=2025-07-09 14:04:02  -> 2025-07-04 14:04:02
```

Flags: `--dry-run`, `--offset-days N` (default `2`; allow assets up to N days before/after the album's date before flagging them), `--interactive`/`-i` (ask once per album instead of one bulk confirmation), `--yes` (skip all prompts — bulk or interactive).

> ⚠️ Without `--interactive`, confirming prints an explicit warning that it will change every listed asset automatically, with no per-album review (e.g. "This changes 1293 asset(s) automatically, without reviewing each one individually").

> ⚠️ **This is the one deliberate exception to this project's rule against deprecated endpoints.** Fixing a date calls `PUT /assets/{id}` (`updateAsset`), the only Immich API that can set an asset's capture date — it is marked deprecated upstream with a self-referential (i.e. non-existent) `replacementId`, so no working replacement exists. See `.github/copilot-instructions.md` for the exact scope of this exception.

### `client-workflow download-album`

Mirrors exactly one album into a local folder — the full originals, or just the small thumbnails when file size matters (e.g. copying a huge album to a phone or a size-limited drive):

1. **Resolve** the album from `--album-id` or `--album-name` (`GET /albums`/`GET /albums/{id}`) — a name must resolve to exactly one album, or the command lists every match and asks you to use `--album-id` instead
2. **Fetch** the album's assets (`POST /search/metadata`, filtered by album), optionally dropping videos (`--ignore-videos`)
3. **Download** each asset as either the full original (`GET /assets/{id}/original`) or its thumbnail (`GET /assets/{id}/thumbnail`, `--size thumbnail`) into `--target-dir`, named after the asset's own original file name (duplicate names within an album get a short, deterministic asset-ID suffix), optionally prefixed with its capture date/time (`--timestamp-prefix`)
4. **Optionally resize** (`--resize`) each downloaded file to JPEG via ImageMagick, before it's written to its final name

Without `--sync` this is a simple one-shot bulk download: every matching asset is (re-)downloaded, always overwriting whatever is already there.

With `--sync`, a hidden manifest (`.immich-album-sync.json`) is kept in `--target-dir` so repeated runs behave like a proper mirror instead of a blind re-download:

- assets not yet in the manifest are downloaded (**add**)
- assets whose checksum changed since the last sync are re-downloaded (**update**)
- assets whose checksum is unchanged are skipped (**unchanged**)
- manifest entries whose asset is no longer in the (filtered) album have their local file **deleted**

Only files this tool itself downloaded (tracked in the manifest) are ever touched or deleted — anything else you put in the target folder is left alone. Change detection compares the *original* asset's checksum even in `--size thumbnail` mode (Immich exposes no separate thumbnail checksum, and a thumbnail is derived deterministically from the original), so a metadata-only edit that doesn't change the original file's bytes (e.g. a pure EXIF rotation) won't trigger a thumbnail re-download. The manifest also records which album, `--size`, `--resize`, and `--timestamp-prefix` it was built with; pointing `--sync` at a folder whose manifest doesn't match any of those for the current run is refused rather than risk deleting/misnaming files or mixing conventions — use a fresh `--target-dir` to switch.

**`--timestamp-prefix`** prefixes each local file name with the asset's capture date/time (from its `LocalDateTime` metadata), formatted `yyyy-MM-dd_HH_mm_ss` (e.g. `2025-07-04_14_04_02_IMG_1234.jpg`), so a plain directory listing sorts chronologically. Assets that still collide after the prefix (e.g. burst shots within the same second) get the same short asset-ID suffix as the no-prefix case.

**`--resize`** re-encodes every downloaded file to JPEG using [ImageMagick](https://imagemagick.org/), regardless of the source format (useful when local disk/transfer size matters more than preserving the exact original format — see `--size thumbnail` for an even smaller alternative). It requires the `magick` (v7+) or `convert` (legacy v6) executable, resolved in this order: `tools.imagemagick_path` in the config file, the `IMMICH_IMAGEMAGICK_PATH` environment variable, then a `PATH` lookup — checked once before any download starts, so a missing tool fails fast. `--resize-width`/`--resize-height` (pixels, either or both; 0 means unconstrained on that axis — ImageMagick fits the image within the box, preserving aspect ratio) control the target size, and `--resize-quality` (1-100, default 85) controls JPEG quality; omitting both width and height just re-encodes/re-compresses without changing dimensions.

> ⚠️ **`--resize` only ever runs against actual image content**, never a video's real file: with `--size original`, non-`IMAGE` assets (videos, audio, anything else) are saved as-is, untouched, because running ImageMagick against a video file treats it as a sequence of frames and would either fail or silently produce one JPEG per frame. With `--size thumbnail`, resize always applies — the thumbnail endpoint always returns a static preview image, even for a video asset. Combine `--resize` with `--ignore-videos` if you want every downloaded file to actually be resized.

```sh
# One-shot download of every original in an album
immich-admin cw download-album --album-name "2025-07-04 Garten" --target-dir ./garten

# Thumbnails only (smaller footprint), skipping videos
immich-admin cw download-album --album-id d4856b75-7700-414c-9dc2-4f6d501936d1 --target-dir ./garten-thumbs --size thumbnail --ignore-videos

# Chronologically-sortable file names
immich-admin cw download-album --album-name "2025-07-04 Garten" --target-dir ./garten --timestamp-prefix

# Resize every downloaded photo to fit within 1920x1080, re-encoded to JPEG at quality 80
immich-admin cw download-album --album-name "2025-07-04 Garten" --target-dir ./garten-1080p --resize --resize-width 1920 --resize-height 1080 --resize-quality 80

# Keep a folder mirrored to the album going forward
immich-admin cw download-album --album-name "2025-07-04 Garten" --target-dir ./garten --sync --dry-run
immich-admin cw download-album --album-name "2025-07-04 Garten" --target-dir ./garten --sync --yes
```

Flags: exactly one of `--album-id UUID` / `--album-name NAME`, `--target-dir DIR` (required), `--size original|thumbnail` (default `original`), `--ignore-videos`, `--timestamp-prefix`, `--resize`, `--resize-width PIXELS`, `--resize-height PIXELS`, `--resize-quality 1-100` (default `85`; the latter three require `--resize`), `--sync`, `--dry-run`, `--yes` (skip the confirmation prompt before `--sync` deletes local files).

The underlying operations are also available standalone: `assets download-original` and the new `assets download-thumbnail` (`GET /assets/{id}/thumbnail`, supporting the full `original|fullsize|preview|thumbnail` `AssetMediaSize` range plus `--edited`).

## Sample Use Cases

### I imported some corrupt images, now I want to replace them, but keep all metadata and albums

**1. Get information about one image**

```console
> immich-admin.exe assets info f63543bb-21bd-4b2a-9f7b-80ee7ef8d1ca
ID:        f63543bb-21bd-4b2a-9f7b-80ee7ef8d1ca
Name:      DSC_4466_DxO.jpg
Type:      IMAGE
Size:      667.4 KiB
Dimension: 4895x3268
Captured:  2018-07-06 10:54:03
Path:      /data/library/daniel/2018-06-20 Vacation/DSC_4466.jpg
Flags:     favorite=false archived=false trashed=false offline=false
```

**2. Replace one or multiple images**

```console
> immich-admin.exe cw replace-asset --replace-file ids.txt
```

`ids.txt` (format `assetId;newFilePath`, one pair per line):

```
f63543bb-21bd-4b2a-9f7b-80ee7ef8d1ca;new-image.jpg
```

This uploads `new-image.jpg` as a new asset, verifies it, copies the original's albums/favorite/shared-link/sidecar/stack metadata onto it, and moves the corrupt original to the trash — see [`client-workflow replace-asset`](#client-workflow-replace-asset-alias-cw) above for the full step-by-step and all available flags.

### I imported photos with immich-go and want to clean out every tag except the immich-go ones

immich-go creates its own tags (e.g. `{immich-go}` and dated sub-tags like `{immich-go}/2026-07-23 18:50:54`). To remove all your other tags but keep those, exclude anything whose path contains `immich-go`:

```console
> immich-admin.exe cw tag-delete --exclude "immich-go" --dry-run
117 tag(s) would be deleted:
  2ce9dc42-edc3-41af-ad02-4cb16035a65d  2026
  44dc4fbc-7732-4e2f-82ac-649d53a8c6df  Amy
  ...
Warning: deletion is PERMANENT (tags have no trash) and deleting a parent tag also deletes its children.
```

`--dry-run` only previews the selection. The `{immich-go}` tags are kept because their full path contains `immich-go`; everything else is listed for deletion. Remove `--dry-run` (and add `--yes` to skip the prompt) to actually delete them. See [`client-workflow tag-delete`](#client-workflow-tag-delete-alias-cw) above for all flags.

> Note: `--exclude "immich-go"` is a **regex**, matched as a substring of the full tag path. To match "contains", write the literal text (`immich-go`), not a glob like `*immich-go*`.

### I want to share all albums containing "Amy" or "Amelia" with a specific user

Use `add-users-to-album-with-pattern`: it resolves the user first, then previews every matching album before sharing anything.

```console
> immich-admin.exe cw add-users-to-album-with-pattern --include "Amy|Amelia" --user Julia --dry-run
Target user: Julia Roberts <julia@example.com> (2a6e9e0e-7a7a-4b3a-9f2e-5c6b2f5a9d11)
3 album(s) would be shared with Julia Roberts as viewer:
  0c1f2b3a-4d5e-6f70-8192-a3b4c5d6e7f8  Amy's Wedding  245 asset(s)
  1d2e3f40-5a6b-7c8d-9e0f-a1b2c3d4e5f6  Amelia Birthday 2024  88 asset(s)
  2e3f4051-6b7c-8d9e-0f1a-b2c3d4e5f6a7  Amelia & Amy Road Trip  512 asset(s)  (already shared)
```

`--dry-run` only previews the selection and resolved user. Remove `--dry-run` (and add `--yes` to skip the confirmation prompt) to actually share. If `--user` matches more than one person (or nobody), the command errors out and lists the candidates so you can narrow it down with an exact email or UUID instead. Albums where the user already has access are skipped automatically. See [`client-workflow add-users-to-album-with-pattern`](#client-workflow-add-users-to-album-with-pattern-alias-cw) above for all flags.

Prefer to decide album-by-album instead of one bulk yes/no? Add `--interactive` (alias `-i`):

```console
> immich-admin.exe cw add-users-to-album-with-pattern --include "Amy|Amelia" --user Julia --interactive
Target user: Julia Roberts <julia@example.com> (2a6e9e0e-7a7a-4b3a-9f2e-5c6b2f5a9d11)
2 album(s) would be shared with Julia Roberts as viewer:
  0c1f2b3a-4d5e-6f70-8192-a3b4c5d6e7f8  Amy's Wedding  245 asset(s)
  1d2e3f40-5a6b-7c8d-9e0f-a1b2c3d4e5f6  Amelia Birthday 2024  88 asset(s)
Share "Amy's Wedding" (245 asset(s)) with Julia Roberts as viewer? [y/N]: y
Share "Amelia Birthday 2024" (88 asset(s)) with Julia Roberts as viewer? [y/N]: n
Amy's Wedding: Share album "Amy's Wedding" (0c1f2b3a-...) with Julia Roberts <julia@example.com> as viewer... ok
```

Each album gets its own yes/no prompt; albums where the user already has access are added automatically without asking (there's nothing to decide). `--yes` always wins over `--interactive` and shares everything without asking.

### I want to find corrupt images that have no thumbnail (thumbhash)

Assets without a thumbhash are a strong indicator of corruption — Immich could not generate a preview. Use `find-no-thumbhash` to identify them:

```console
> immich-admin.exe cw find-no-thumbhash --type IMAGE
Scanned 86911 assets, found 191 without thumbhash...
Found 191 asset(s) without thumbhash:
43da46e2-d414-4703-8efb-dfc40f8acc7b    20241026_143206(1).dng  IMAGE
467e8ce5-1aef-47f0-813f-6cd39607fc85    original_…_IMG_20210728_075509.jpg    IMAGE
d2baa3b7-f61c-4434-bafd-a5a86856491e    Wintertraum_ST_321.jpg  IMAGE
...
```

Save the IDs to a file for use with a future repair workflow:

```console
> immich-admin.exe cw find-no-thumbhash --type IMAGE -q > corrupt-ids.txt
> wc -l corrupt-ids.txt
191 corrupt-ids.txt
```

Pre-filter by file extension to narrow the scan:

```console
> immich-admin.exe cw find-no-thumbhash --original-file-name JPG -q
467e8ce5-1aef-47f0-813f-6cd39607fc85
02348d7c-35a1-454e-bd5d-5650974b0d1e
...
```

### I imported JPEGs that Immich shows as broken (no thumbnail) and want to repair them in place

Many imported JPEGs are corrupt only in that they are missing the mandatory End-of-Image marker (`FF D9`) — the pixels are all there, so the fix is to append two bytes and re-import. `repair-assets` does this end to end and keeps every album/favorite/metadata. Always preview with `--dry-run` first:

```console
> immich-admin.exe cw repair-assets --mode marker --check-all-assets --dry-run
Found 191 asset(s) without thumbhash to attempt repair.
d2baa3b7-f61c-4434-bafd-a5a86856491e: applying "marker" repair, re-importing repaired file
[dry-run] d2baa3b7-…: would run 4 step(s):
[dry-run]   1) Upload new file …_repaired.jpg
[dry-run]   2) Verify upload (checksum matches local file)
[dry-run]   3) Copy metadata from original asset
[dry-run]   4) Remove original asset
...
Summary: repaired=0 already-ok=0 skipped-unsupported=0 unrepairable=0 (of 191)
```

Then run it for real. The original is trashed as soon as the repaired replacement is uploaded, checksum-verified, and has its metadata copied — this does **not** wait for Immich to generate a thumbnail (see the note above); re-check with `find-no-thumbhash` afterwards if you want to confirm:

```console
> immich-admin.exe cw repair-assets --mode all --check-all-assets --yes
```

You can also repair a fixed list of IDs (e.g. the `corrupt-ids.txt` from above), keep the originals with `--keep-original`, or permanently delete them with `--force`. Assets with no applicable strategy for the mode are skipped and reported. See [`client-workflow repair-assets`](#client-workflow-repair-assets-alias-cw) above for all flags and the full verification model.

### I have TIFFs that Immich can't thumbnail because of an invalid zero-count IFD tag

Some cameras/scanners write a private TIFF tag with its count field set to `0`, which libtiff (Immich's thumbnailer) rejects fatally even though the pixel data is intact. `--mode tiff-tags` detects this exact defect (a structural IFD-chain walk, not a blind "it's a TIFF" guess) and patches only the offending 4-byte count fields — file size, layout, pixel data and metadata are all unchanged:

```console
> immich-admin.exe cw repair-assets --mode tiff-tags --check-all-assets --dry-run
Found 21 asset(s) without thumbhash to attempt repair.
08662bc6-d049-409b-b608-4c8f69076d02: applying "tiff-zero-count" repair, re-importing repaired file
[dry-run] 08662bc6-…: would run 4 step(s):
...
Summary: repaired=0 already-ok=0 skipped-unsupported=0 unrepairable=0 (of 21)
```

`--mode all` runs both `marker` and `tiff-tags` in one pass, so mixed libraries with both corrupt JPEGs and corrupt TIFFs need only one command.

### Some "photos" are actually Google Takeout JSON sidecars with no image — I want to remove them

A known Google Photos Takeout failure imports a photo's `*.json` metadata sidecar *in place of* the real picture: the asset's bytes are pure JSON (`{ "title": "...", "photoTakenTime": {...}, ... }`) with no image data, so Immich can never thumbnail it and there is **nothing to repair** — the pixels are gone. `--mode takeout-json` finds these with a strict structural check (the leading JSON must carry the full Google fingerprint) and deletes them. Always preview with `--dry-run` first:

```console
> immich-admin.exe cw repair-assets --mode takeout-json --check-all-assets --dry-run
Found 26 asset(s) without thumbhash to attempt repair.
34be3430-…: confirmed Google Takeout JSON sidecar (original title "_MG_1366.jpg", 699-byte JSON header); deleting (trash)
[dry-run] 34be3430-…: would delete corrupt sidecar asset (trash)
961ff72e-…: skipped (VIDEO "PXL_20230807_145334662.MP.mp4" is not a Google Takeout JSON sidecar)
...
Summary: deleted-sidecar=25 skipped-not-sidecar=1 (of 26)
```

Note how a genuine video that merely happened to lack a thumbhash is reported **skipped-not-sidecar** and left untouched — only files that truly are sidecars are ever deleted. Drop `--dry-run` and add `--yes` to move them to trash (or add `--force` to delete permanently):

```console
> immich-admin.exe cw repair-assets --mode takeout-json --check-all-assets --yes
```

### I only want to repair the broken images inside one specific album

Pass the album's ID with `--album-id`. The album is validated and its name is shown, then only that album's corrupt (no-thumbhash) images are repaired — everything else is left alone:

```console
> immich-admin.exe cw repair-assets --mode all --album-id 6f2a1c3e-… --dry-run
Album "Wintertraum 2024" (312 asset(s)); scanning for IMAGE assets without thumbhash...
Found 18 asset(s) without thumbhash to attempt repair.
...
Summary: repaired=0 already-ok=0 skipped-unsupported=0 unrepairable=0 (of 18)
```

Drop `--dry-run` and add `--yes` to run it for real. The same `--album-id` filter also works on the read-only `find-no-thumbhash` if you just want to *list* an album's broken images first:

```console
> immich-admin.exe cw find-no-thumbhash --album-id 6f2a1c3e-… --type IMAGE -q
```

### I want to find all assets with a specific file extension, just like the Immich web search

The Immich web UI lets you search by file name via the search bar (e.g. `https://immich.example.com/search?query={"originalFileName":"JPG"}`). The same query works on the command line:

```console
> immich-admin.exe search metadata --original-file-name JPG --page-size 10
b847ca61-2542-4ec3-9d86-136b2ad64104    20260722_195350.jpg     IMAGE
d47c1709-3540-42e5-a735-b6e9279b6190    20260722_190947.jpg     IMAGE
a464ee03-ca9b-4567-a873-e269ea26f6f9    20260722_073838(0).jpg  IMAGE
...
--- 10 asset(s) found (page 1) ---
```

Use `--all` to page through the entire library automatically:

```console
> immich-admin.exe search metadata --original-file-name JPG --all
... (all matching assets across all pages)
--- 3842 asset(s) found (page 39) ---
```

### I want a list of IDs for a filtered set of assets, to pipe into another command

`--ids-only` (short: `-q`) prints one UUID per line — ready to be piped into `assets info`, `cw replace-asset`, or saved to a file for later use:

```console
> immich-admin.exe search metadata --original-file-name JPG -q | Select-Object -First 2
b847ca61-2542-4ec3-9d86-136b2ad64104
d47c1709-3540-42e5-a735-b6e9279b6190
```

Pipe directly into `assets info` to inspect each result:

```console
> immich-admin.exe search metadata --original-file-name JPG -q | ForEach-Object {
    immich-admin.exe assets info $_
}
```

Or save the IDs to a file for use with bulk commands (`--ids-file`):

```console
> immich-admin.exe search metadata --original-file-name JPG --all -q > corrupt-jpgs.txt
> immich-admin.exe cw replace-asset --replace-file pairs.txt   # pairs: "oldId;newFile"
```

### I want to find all images that are not in any album

```console
> immich-admin.exe search metadata --is-not-in-album true --type IMAGE --all -q > orphans.txt
```

### I want to find all offline assets (files that Immich can no longer access)

```console
> immich-admin.exe search metadata --is-offline true --all
```

### I want to filter by camera make and model

```console
> immich-admin.exe search metadata --make Canon --model "EOS R5" --all -q > canon-r5.txt
```

### I want to filter by location and date range

```console
> immich-admin.exe search metadata --city Berlin --taken-after 2024-01-01T00:00:00Z --taken-before 2024-12-31T23:59:59Z
```

### I want to keep a local folder mirrored to an album, using small thumbnails to save space

```console
> immich-admin.exe cw download-album --album-name "2025-07-04 Garten" --target-dir D:\Photos\Garten --size thumbnail --ignore-videos --sync --dry-run
Album "2025-07-04 Garten" (d4856b75-7700-414c-9dc2-4f6d501936d1): 245 to add, 0 to update, 0 unchanged, 0 to remove
  + IMG_1234.jpg (a1b2c3d4-...)
  ...
> immich-admin.exe cw download-album --album-name "2025-07-04 Garten" --target-dir D:\Photos\Garten --size thumbnail --ignore-videos --sync --yes
```

Re-running the same command later only downloads what changed and removes local files for photos taken out of the album — everything else is left untouched. See [`client-workflow download-album`](#client-workflow-download-album) above for all flags and the manifest/safety model.

## API Coverage

<!-- Generated by tools/apitable — do not edit between the markers. Refresh with `go generate ./...` -->
<!-- API-TABLE:BEGIN -->
**19 of 235 endpoints implemented** (17 deprecated and 2 internal endpoints omitted per project policy).

<details>
<summary><b>API keys</b> (0/5)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/api-keys` | `getApiKeys` | Stable |
|  | POST | `/api-keys` | `createApiKey` | Stable |
|  | GET | `/api-keys/me` | `getMyApiKey` | Stable |
|  | DELETE | `/api-keys/{id}` | `deleteApiKey` | Stable |
|  | GET | `/api-keys/{id}` | `getApiKey` | Stable |

</details>

<details>
<summary><b>Activities</b> (0/4)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/activities` | `getActivities` | Stable |
|  | POST | `/activities` | `createActivity` | Stable |
|  | GET | `/activities/statistics` | `getActivityStatistics` | Stable |
|  | DELETE | `/activities/{id}` | `deleteActivity` | Stable |

</details>

<details>
<summary><b>Albums</b> (3/13)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
| ✅ | GET | `/albums` | `getAllAlbums` | Stable |
|  | POST | `/albums` | `createAlbum` | Stable |
|  | PUT | `/albums/assets` | `addAssetsToAlbums` | Stable |
|  | GET | `/albums/statistics` | `getAlbumStatistics` | Stable |
|  | DELETE | `/albums/{id}` | `deleteAlbum` | Stable |
| ✅ | GET | `/albums/{id}` | `getAlbumInfo` | Stable |
|  | PATCH | `/albums/{id}` | `updateAlbumInfo` | Stable |
|  | DELETE | `/albums/{id}/assets` | `removeAssetFromAlbum` | Stable |
|  | PUT | `/albums/{id}/assets` | `addAssetsToAlbum` | Stable |
|  | GET | `/albums/{id}/map-markers` | `getAlbumMapMarkers` | – |
|  | DELETE | `/albums/{id}/user/{userId}` | `removeUserFromAlbum` | Stable |
|  | PUT | `/albums/{id}/user/{userId}` | `updateAlbumUser` | Stable |
| ✅ | PUT | `/albums/{id}/users` | `addUsersToAlbum` | Stable |

</details>

<details>
<summary><b>Assets</b> (6/24)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
| ✅ | DELETE | `/assets` | `deleteAssets` | Stable |
| ✅ | POST | `/assets` | `uploadAsset` | Stable |
|  | POST | `/assets/bulk-upload-check` | `checkBulkUpload` | Stable |
| ✅ | PUT | `/assets/copy` | `copyAsset` | Stable |
|  | POST | `/assets/jobs` | `runAssetJobs` | Stable |
|  | DELETE | `/assets/metadata` | `deleteBulkAssetMetadata` | Beta |
|  | PUT | `/assets/metadata` | `updateBulkAssetMetadata` | Beta |
|  | GET | `/assets/statistics` | `getAssetStatistics` | Stable |
| ✅ | GET | `/assets/{id}` | `getAssetInfo` | Stable |
|  | DELETE | `/assets/{id}/edits` | `removeAssetEdits` | Beta |
|  | GET | `/assets/{id}/edits` | `getAssetEdits` | Beta |
|  | PUT | `/assets/{id}/edits` | `editAsset` | Beta |
|  | GET | `/assets/{id}/metadata` | `getAssetMetadata` | Stable |
|  | PUT | `/assets/{id}/metadata` | `updateAssetMetadata` | Stable |
|  | DELETE | `/assets/{id}/metadata/{key}` | `deleteAssetMetadata` | Stable |
|  | GET | `/assets/{id}/metadata/{key}` | `getAssetMetadataByKey` | Stable |
|  | GET | `/assets/{id}/ocr` | `getAssetOcr` | Stable |
| ✅ | GET | `/assets/{id}/original` | `downloadAsset` | Stable |
| ✅ | GET | `/assets/{id}/thumbnail` | `viewAsset` | Stable |
|  | GET | `/assets/{id}/video/playback` | `playAssetVideo` | Stable |
|  | GET | `/assets/{id}/video/stream/main.m3u8` | `getMainPlaylist` | Alpha |
|  | DELETE | `/assets/{id}/video/stream/{sessionId}` | `endSession` | Alpha |
|  | GET | `/assets/{id}/video/stream/{sessionId}/{variantIndex}/playlist.m3u8` | `getMediaPlaylist` | Alpha |
|  | GET | `/assets/{id}/video/stream/{sessionId}/{variantIndex}/{filename}` | `getSegment` | Alpha |

</details>

<details>
<summary><b>Authentication</b> (0/17)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | POST | `/auth/admin-sign-up` | `signUpAdmin` | Stable |
|  | POST | `/auth/change-password` | `changePassword` | Stable |
|  | POST | `/auth/login` | `login` | Stable |
|  | POST | `/auth/logout` | `logout` | Stable |
|  | DELETE | `/auth/pin-code` | `resetPinCode` | Stable |
|  | POST | `/auth/pin-code` | `setupPinCode` | Stable |
|  | PUT | `/auth/pin-code` | `changePinCode` | Stable |
|  | POST | `/auth/session/lock` | `lockAuthSession` | Stable |
|  | POST | `/auth/session/unlock` | `unlockAuthSession` | Stable |
|  | GET | `/auth/status` | `getAuthStatus` | Stable |
|  | POST | `/auth/validateToken` | `validateAccessToken` | Stable |
|  | POST | `/oauth/authorize` | `startOAuth` | Stable |
|  | POST | `/oauth/backchannel-logout` | `logoutOAuth` | – |
|  | POST | `/oauth/callback` | `finishOAuth` | Stable |
|  | POST | `/oauth/link` | `linkOAuthAccount` | Stable |
|  | GET | `/oauth/mobile-redirect` | `redirectOAuthToMobile` | Stable |
|  | POST | `/oauth/unlink` | `unlinkOAuthAccount` | Stable |

</details>

<details>
<summary><b>Authentication (admin)</b> (0/1)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | POST | `/admin/auth/unlink-all` | `unlinkAllOAuthAccountsAdmin` | Stable |

</details>

<details>
<summary><b>Database Backups (admin)</b> (0/5)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | DELETE | `/admin/database-backups` | `deleteDatabaseBackup` | Alpha |
|  | GET | `/admin/database-backups` | `listDatabaseBackups` | Alpha |
|  | POST | `/admin/database-backups/start-restore` | `startDatabaseRestoreFlow` | Alpha |
|  | POST | `/admin/database-backups/upload` | `uploadDatabaseBackup` | Alpha |
|  | GET | `/admin/database-backups/{filename}` | `downloadDatabaseBackup` | Alpha |

</details>

<details>
<summary><b>Download</b> (0/2)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | POST | `/download/archive` | `downloadArchive` | Stable |
|  | POST | `/download/info` | `getDownloadInfo` | Stable |

</details>

<details>
<summary><b>Duplicates</b> (0/4)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | DELETE | `/duplicates` | `deleteDuplicates` | Stable |
|  | GET | `/duplicates` | `getAssetDuplicates` | Stable |
|  | POST | `/duplicates/resolve` | `resolveDuplicates` | Alpha |
|  | DELETE | `/duplicates/{id}` | `deleteDuplicate` | Stable |

</details>

<details>
<summary><b>Faces</b> (0/4)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/faces` | `getFaces` | Stable |
|  | POST | `/faces` | `createFace` | Stable |
|  | DELETE | `/faces/{id}` | `deleteFace` | Stable |
|  | PUT | `/faces/{id}` | `reassignFacesById` | Stable |

</details>

<details>
<summary><b>Jobs</b> (0/1)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | POST | `/jobs` | `createJob` | Stable |

</details>

<details>
<summary><b>Libraries</b> (0/7)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/libraries` | `getAllLibraries` | Stable |
|  | POST | `/libraries` | `createLibrary` | Stable |
|  | DELETE | `/libraries/{id}` | `deleteLibrary` | Stable |
|  | GET | `/libraries/{id}` | `getLibrary` | Stable |
|  | POST | `/libraries/{id}/scan` | `scanLibrary` | Stable |
|  | GET | `/libraries/{id}/statistics` | `getLibraryStatistics` | Stable |
|  | POST | `/libraries/{id}/validate` | `validate` | Stable |

</details>

<details>
<summary><b>Maintenance (admin)</b> (0/9)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/admin/integrity/report` | `getIntegrityReport` | Alpha |
|  | DELETE | `/admin/integrity/report/{id}` | `deleteIntegrityReport` | Alpha |
|  | GET | `/admin/integrity/report/{id}/file` | `getIntegrityReportFile` | Alpha |
|  | GET | `/admin/integrity/report/{type}/csv` | `getIntegrityReportCsv` | Alpha |
|  | GET | `/admin/integrity/summary` | `getIntegrityReportSummary` | Alpha |
|  | POST | `/admin/maintenance` | `setMaintenanceMode` | Alpha |
|  | GET | `/admin/maintenance/detect-install` | `detectPriorInstall` | Alpha |
|  | POST | `/admin/maintenance/login` | `maintenanceLogin` | Alpha |
|  | GET | `/admin/maintenance/status` | `getMaintenanceStatus` | Alpha |

</details>

<details>
<summary><b>Map</b> (0/2)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/map/markers` | `getMapMarkers` | Stable |
|  | GET | `/map/reverse-geocode` | `reverseGeocode` | Stable |

</details>

<details>
<summary><b>Memories</b> (0/7)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/memories` | `searchMemories` | Stable |
|  | POST | `/memories` | `createMemory` | Stable |
|  | GET | `/memories/statistics` | `memoriesStatistics` | Stable |
|  | DELETE | `/memories/{id}` | `deleteMemory` | Stable |
|  | GET | `/memories/{id}` | `getMemory` | Stable |
|  | DELETE | `/memories/{id}/assets` | `removeMemoryAssets` | Stable |
|  | PUT | `/memories/{id}/assets` | `addMemoryAssets` | Stable |

</details>

<details>
<summary><b>Notifications</b> (0/6)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | DELETE | `/notifications` | `deleteNotifications` | Stable |
|  | GET | `/notifications` | `getNotifications` | Stable |
|  | PUT | `/notifications` | `updateNotifications` | Stable |
|  | DELETE | `/notifications/{id}` | `deleteNotification` | Stable |
|  | GET | `/notifications/{id}` | `getNotification` | Stable |
|  | PUT | `/notifications/{id}` | `updateNotification` | Stable |

</details>

<details>
<summary><b>Notifications (admin)</b> (0/3)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | POST | `/admin/notifications` | `createNotification` | Stable |
|  | POST | `/admin/notifications/templates/{name}` | `getNotificationTemplateAdmin` | Stable |
|  | POST | `/admin/notifications/test-email` | `sendTestEmailAdmin` | Stable |

</details>

<details>
<summary><b>Partners</b> (0/4)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/partners` | `getPartners` | Stable |
|  | POST | `/partners` | `createPartner` | Stable |
|  | DELETE | `/partners/{id}` | `removePartner` | Stable |
|  | PUT | `/partners/{id}` | `updatePartner` | Stable |

</details>

<details>
<summary><b>People</b> (0/10)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | DELETE | `/people` | `deletePeople` | Stable |
|  | GET | `/people` | `getAllPeople` | Stable |
|  | POST | `/people` | `createPerson` | Stable |
|  | PUT | `/people` | `updatePeople` | Stable |
|  | DELETE | `/people/{id}` | `deletePerson` | Stable |
|  | GET | `/people/{id}` | `getPerson` | Stable |
|  | POST | `/people/{id}/merge` | `mergePerson` | Stable |
|  | PUT | `/people/{id}/reassign` | `reassignFaces` | Stable |
|  | GET | `/people/{id}/statistics` | `getPersonStatistics` | Stable |
|  | GET | `/people/{id}/thumbnail` | `getPersonThumbnail` | Stable |

</details>

<details>
<summary><b>Plugins</b> (0/4)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/plugins` | `searchPlugins` | – |
|  | GET | `/plugins/methods` | `searchPluginMethods` | – |
|  | GET | `/plugins/templates` | `searchPluginTemplates` | – |
|  | GET | `/plugins/{id}` | `getPlugin` | – |

</details>

<details>
<summary><b>Queues</b> (0/5)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/queues` | `getQueues` | Alpha |
|  | GET | `/queues/{name}` | `getQueue` | Alpha |
|  | PUT | `/queues/{name}` | `updateQueue` | Alpha |
|  | DELETE | `/queues/{name}/jobs` | `emptyQueue` | Alpha |
|  | GET | `/queues/{name}/jobs` | `getQueueJobs` | Alpha |

</details>

<details>
<summary><b>Search</b> (1/10)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/search/cities` | `getAssetsByCity` | Stable |
|  | GET | `/search/explore` | `getExploreData` | Stable |
|  | POST | `/search/large-assets` | `searchLargeAssets` | Stable |
| ✅ | POST | `/search/metadata` | `searchAssets` | Stable |
|  | GET | `/search/person` | `searchPerson` | Stable |
|  | GET | `/search/places` | `searchPlaces` | Stable |
|  | POST | `/search/random` | `searchRandom` | Stable |
|  | POST | `/search/smart` | `searchSmart` | Stable |
|  | POST | `/search/statistics` | `searchAssetStatistics` | Stable |
|  | GET | `/search/suggestions` | `getSearchSuggestions` | Stable |

</details>

<details>
<summary><b>Server</b> (0/14)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/server/about` | `getAboutInfo` | Stable |
|  | GET | `/server/apk-links` | `getApkLinks` | Stable |
|  | GET | `/server/config` | `getServerConfig` | Stable |
|  | GET | `/server/features` | `getServerFeatures` | Stable |
|  | DELETE | `/server/license` | `deleteServerLicense` | Stable |
|  | GET | `/server/license` | `getServerLicense` | Stable |
|  | PUT | `/server/license` | `setServerLicense` | Stable |
|  | GET | `/server/media-types` | `getSupportedMediaTypes` | Stable |
|  | GET | `/server/ping` | `pingServer` | Stable |
|  | GET | `/server/statistics` | `getServerStatistics` | Stable |
|  | GET | `/server/storage` | `getStorage` | Stable |
|  | GET | `/server/version` | `getServerVersion` | Stable |
|  | GET | `/server/version-check` | `getVersionCheck` | Stable |
|  | GET | `/server/version-history` | `getVersionHistory` | Stable |

</details>

<details>
<summary><b>Sessions</b> (0/5)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | DELETE | `/sessions` | `deleteAllSessions` | Stable |
|  | GET | `/sessions` | `getSessions` | Stable |
|  | POST | `/sessions` | `createSession` | Stable |
|  | DELETE | `/sessions/{id}` | `deleteSession` | Stable |
|  | POST | `/sessions/{id}/lock` | `lockSession` | Stable |

</details>

<details>
<summary><b>Shared links</b> (0/9)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/shared-links` | `getAllSharedLinks` | Stable |
|  | POST | `/shared-links` | `createSharedLink` | Stable |
|  | POST | `/shared-links/login` | `sharedLinkLogin` | Beta |
|  | GET | `/shared-links/me` | `getMySharedLink` | Stable |
|  | DELETE | `/shared-links/{id}` | `removeSharedLink` | Stable |
|  | GET | `/shared-links/{id}` | `getSharedLinkById` | Stable |
|  | PATCH | `/shared-links/{id}` | `updateSharedLink` | Stable |
|  | DELETE | `/shared-links/{id}/assets` | `removeSharedLinkAssets` | Stable |
|  | PUT | `/shared-links/{id}/assets` | `addSharedLinkAssets` | Stable |

</details>

<details>
<summary><b>Stacks</b> (0/6)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | DELETE | `/stacks` | `deleteStacks` | Stable |
|  | GET | `/stacks` | `searchStacks` | Stable |
|  | POST | `/stacks` | `createStack` | Stable |
|  | DELETE | `/stacks/{id}` | `deleteStack` | Stable |
|  | GET | `/stacks/{id}` | `getStack` | Stable |
|  | DELETE | `/stacks/{id}/assets/{assetId}` | `removeAssetFromStack` | Stable |

</details>

<details>
<summary><b>Sync</b> (0/4)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | DELETE | `/sync/ack` | `deleteSyncAck` | Stable |
|  | GET | `/sync/ack` | `getSyncAck` | Stable |
|  | POST | `/sync/ack` | `sendSyncAck` | Stable |
|  | POST | `/sync/stream` | `getSyncStream` | Stable |

</details>

<details>
<summary><b>System config</b> (0/4)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/system-config` | `getConfig` | Stable |
|  | PUT | `/system-config` | `updateConfig` | Stable |
|  | GET | `/system-config/defaults` | `getConfigDefaults` | Stable |
|  | GET | `/system-config/storage-template-options` | `getStorageTemplateOptions` | Stable |

</details>

<details>
<summary><b>System metadata</b> (0/4)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/system-metadata/admin-onboarding` | `getAdminOnboarding` | Stable |
|  | POST | `/system-metadata/admin-onboarding` | `updateAdminOnboarding` | Stable |
|  | GET | `/system-metadata/reverse-geocoding-state` | `getReverseGeocodingState` | Stable |
|  | GET | `/system-metadata/version-check-state` | `getVersionCheckState` | Stable |

</details>

<details>
<summary><b>Tags</b> (6/8)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
| ✅ | GET | `/tags` | `getAllTags` | Stable |
|  | POST | `/tags` | `createTag` | Stable |
| ✅ | PUT | `/tags` | `upsertTags` | Stable |
| ✅ | PUT | `/tags/assets` | `bulkTagAssets` | Stable |
| ✅ | DELETE | `/tags/{id}` | `deleteTag` | Stable |
| ✅ | GET | `/tags/{id}` | `getTagById` | Stable |
|  | DELETE | `/tags/{id}/assets` | `untagAssets` | Stable |
| ✅ | PUT | `/tags/{id}/assets` | `tagAssets` | Stable |

</details>

<details>
<summary><b>Trash</b> (0/3)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | POST | `/trash/empty` | `emptyTrash` | Stable |
|  | POST | `/trash/restore` | `restoreTrash` | Stable |
|  | POST | `/trash/restore/assets` | `restoreAssets` | Stable |

</details>

<details>
<summary><b>Users</b> (3/14)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
| ✅ | GET | `/users` | `searchUsers` | Stable |
| ✅ | GET | `/users/me` | `getMyUser` | Stable |
|  | GET | `/users/me/calendar-heatmap` | `getMyCalendarHeatmap` | Stable |
|  | DELETE | `/users/me/license` | `deleteUserLicense` | Stable |
|  | GET | `/users/me/license` | `getUserLicense` | Stable |
|  | PUT | `/users/me/license` | `setUserLicense` | Stable |
|  | DELETE | `/users/me/onboarding` | `deleteUserOnboarding` | Stable |
|  | GET | `/users/me/onboarding` | `getUserOnboarding` | Stable |
|  | PUT | `/users/me/onboarding` | `setUserOnboarding` | Stable |
|  | GET | `/users/me/preferences` | `getMyPreferences` | Stable |
|  | DELETE | `/users/profile-image` | `deleteProfileImage` | Stable |
|  | POST | `/users/profile-image` | `createProfileImage` | Stable |
| ✅ | GET | `/users/{id}` | `getUser` | Stable |
|  | GET | `/users/{id}/profile-image` | `getProfileImage` | Stable |

</details>

<details>
<summary><b>Users (admin)</b> (0/9)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/admin/users` | `searchUsersAdmin` | Stable |
|  | POST | `/admin/users` | `createUserAdmin` | Stable |
|  | DELETE | `/admin/users/{id}` | `deleteUserAdmin` | Stable |
|  | GET | `/admin/users/{id}` | `getUserAdmin` | Stable |
|  | GET | `/admin/users/{id}/calendar-heatmap` | `getUserCalendarHeatmapAdmin` | Stable |
|  | GET | `/admin/users/{id}/preferences` | `getUserPreferencesAdmin` | Stable |
|  | POST | `/admin/users/{id}/restore` | `restoreUserAdmin` | Stable |
|  | GET | `/admin/users/{id}/sessions` | `getUserSessionsAdmin` | Stable |
|  | GET | `/admin/users/{id}/statistics` | `getUserStatisticsAdmin` | Stable |

</details>

<details>
<summary><b>Views</b> (0/2)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/view/folder` | `getAssetsByOriginalPath` | Stable |
|  | GET | `/view/folder/unique-paths` | `getUniqueOriginalPaths` | Stable |

</details>

<details>
<summary><b>Workflows</b> (0/6)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/workflows` | `searchWorkflows` | – |
|  | POST | `/workflows` | `createWorkflow` | – |
|  | GET | `/workflows/triggers` | `getWorkflowTriggers` | – |
|  | DELETE | `/workflows/{id}` | `deleteWorkflow` | – |
|  | GET | `/workflows/{id}` | `getWorkflow` | – |
|  | GET | `/workflows/{id}/share` | `getWorkflowForShare` | – |

</details>
<!-- API-TABLE:END -->

