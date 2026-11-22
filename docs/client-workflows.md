# Client Workflow Guide

[Back to the README](../README.md)

Client workflows (`client-workflow`, alias `cw`) combine several Immich API calls and local processing into one task. They run on the client and are distinct from Immich's server-side Workflows API (`immich-workflow`).

## Safety Model

Run destructive workflows with `--dry-run` first. Workflows show their planned actions, verify a replacement upload before changing the original, and normally move originals to trash. Use `--yes` only after reviewing the preview; `--force` makes deletion permanent where a command supports it.

## Available Workflows

| Command | Purpose |
|---------|---------|
| `cw replace-asset` | Replace an asset while preserving its albums, favorite state, shared links, sidecar, and stack association |
| `cw tag-delete` | Permanently delete tags selected by include/exclude regular expressions |
| `cw add-users-to-album-with-pattern` | Share every matching album with a resolved user |
| `cw find-no-thumbhash` | Find assets for which Immich has no thumbnail hash |
| `cw repair-assets` | Repair supported JPEG/TIFF defects or delete confirmed Google Takeout JSON sidecars |
| `cw fix-album-dates` | Check date-named albums and optionally correct exact-day capture dates |
| `cw date-from-ocr` | Set capture years from a year printed in the image (OCR), per asset or interactively per album |
| `cw tz-audit` | Report assets per time zone class (none, UTC, offset, IANA, DST mismatch); read-only |
| `cw tz-set` | Give assets without any time zone an IANA zone's offset, keeping the shown time |
| `cw download-album` | Export or synchronize an album to a local folder |
| `cw find-similar` | Find visually similar Immich assets for a local image through immich-clip-probe |
| `cw merge-album` | Merge album contents and metadata |
| `cw watch-upload` / `cw watch-download` | Watch local folders for continuous import or download work |

Planned re-encoding workflows (`reencode-jxl` and `reencode-jpegli`) build on `replace-asset` once their external encoders are available.

## Replace an Asset

`replace-asset` performs these steps in order:

1. Upload the replacement file.
2. Verify that Immich created it and its checksum matches the local replacement.
3. Copy the original metadata and relationships.
4. Trash the original, or permanently delete it with `--force`.

```sh
# One replacement
immich-admin cw replace-asset <ASSET_ID> <NEW_FILE_PATH>

# Multiple replacements: assetId;newFilePath per line
immich-admin cw replace-asset --replace-file pairs.txt
```

Use `--dont-remove-original-file` to keep the original. If Immich reports the upload as an existing duplicate, the workflow stops for that asset rather than act on the wrong record.

## Bulk Tags and Album Sharing

`tag-delete` matches full tag paths with `--include` and `--exclude` regular expressions. Tag deletion is permanent and deleting a parent can delete its children, so preview it first:

```sh
immich-admin cw tag-delete --exclude "immich-go" --dry-run
```

`add-users-to-album-with-pattern` resolves `--user` as either an exact UUID or a case-insensitive name/email match. It lists matching albums before sharing them and skips albums the user can already access.

```sh
immich-admin cw add-users-to-album-with-pattern \
  --include "Amy|Amelia" --user Julia --dry-run
```

Add `--interactive` to decide per album, or choose `--role editor|viewer|owner` to set the granted role.

## Diagnose and Repair Assets

`find-no-thumbhash` is read-only. It scans metadata with pagination and identifies assets whose thumbhash is empty or null. Its `--ids-only` output is intended for pipelines and repair input files.

```sh
immich-admin cw find-no-thumbhash --type IMAGE --ids-only > corrupt-ids.txt
immich-admin cw repair-assets --mode all --ids-file corrupt-ids.txt --dry-run
```

`repair-assets` makes targeted structural repairs only:

| Mode | Behavior |
|------|----------|
| `marker` | Appends a missing JPEG `FF D9` end marker |
| `tiff-tags` | Changes TIFF IFD entries whose invalid count is exactly zero to one |
| `takeout-json` | Trashes a confirmed, unrecoverable Google Takeout JSON sidecar |
| `all` | Runs the safe JPEG and TIFF repair modes; excludes deletion |

The repair workflow never guesses from file extensions alone. TIFF repair requires a valid IFD-chain walk and an actual zero-count entry. `takeout-json` requires a full Google Takeout fingerprint (`title`, both timestamp fields, and `googlePhotosOrigin`) before it will delete an asset.

Repairs download, detect, modify, upload, checksum-verify, copy metadata, then remove the original. They do not wait for asynchronous thumbnail generation; rerun `find-no-thumbhash` afterwards to confirm server processing. Use `--keep-original` when you want to retain the source after a successful repair.

```sh
# Preview all safe repairs for every affected image
immich-admin cw repair-assets --mode all --check-all-assets --dry-run

# Repair a TIFF defect in a selected album
immich-admin cw repair-assets --mode tiff-tags --album-id <ALBUM_ID> --yes

# Preview deletion of confirmed Takeout sidecars
immich-admin cw repair-assets --mode takeout-json --check-all-assets --dry-run
```

## Fix Dates in Date-Named Albums

`fix-album-dates` recognizes albums named `yyyy-MM-dd <title>` or `yyyy <title>`. It compares `LocalDateTime`, Immich's timezone-naive photographer-local timestamp, to the date implied by the album name. Use `--offset-days` to tolerate near-boundary camera or timezone errors.

Exact-day albums can be corrected while retaining each asset's time of day; year-only albums are report-only. Dates are set through `updateAsset` (`PUT /assets/{id}`).

```sh
# Report first
immich-admin cw fix-album-dates --dry-run

# Review each album before changing it
immich-admin cw fix-album-dates --interactive
```

Without `--interactive`, one confirmation applies to every reported asset. Use `--yes` only when that bulk behavior is intended.

## Set Capture Years from OCR

`date-from-ocr` is for scanned or re-photographed prints whose real year is printed in the image. It reads Immich's OCR boxes (`assets ocr`) and finds standalone 4-digit years. An asset with exactly one year that differs from its capture year is a `change` candidate: its date becomes `YYYY-06-01 00:00` (mid-year) and it is tagged `immich-admin-cli/date-from-ocr`. Several distinct years are `ambiguous` and never changed automatically. Every hit is printed with its web link; asset links are accepted as input too.

```sh
# Report only (default): status, OCR text, old -> new date, link
immich-admin cw date-from-ocr --album-name "Scans 1980er"

# Apply only the unambiguous single-asset changes
immich-admin cw date-from-ocr --album-name "Scans 1980er" --yes

# Review hits grouped by album (terminal required)
immich-admin cw date-from-ocr https://immich.example.com/photos/<id> --interactive

# Whole library: one OCR search per year, then review (skips assets whose year already matches)
immich-admin cw date-from-ocr --all --interactive
```

`--interactive` offers `[s]kip`, `[t]his -> YYYY`, `[a]lbum -> YYYY`, `[n]ext album` (skip the rest of this album), `[y]ear...` (type another year), `[o]pen` (browser) and `[q]uit`. `[a]lbum` lists every asset of the album with its current and new date and asks again (default No); assets that are already in that year are kept, and the propagated ones are tagged `immich-admin-cli/date-from-ocr-album`. `--all` finds many numbers that are not dates (e.g. "seit 1938", "2000 qm"), so it is refused together with `--yes`; review the `--all --json` report or use `--all --interactive`. Album-wide changes are not available outside `--interactive`. Like `fix-album-dates`, this sets dates through `updateAsset`.

## Audit and Set Time Zones

`tz-audit` (read-only) searches the library with EXIF data (narrow it with the `search metadata` filter flags) and counts assets per `exifInfo.timeZone` class, with the top cameras per class, then lists every distinct time zone value with its asset count (`(none)` = no zone; also `timeZones` in `--json`):

| Class | Meaning |
|-------|---------|
| `none` | No time zone at all: EXIF without offset and no GPS. Fix with `tz-set`. |
| `utc` | Plain `UTC`: EXIF had no offset, so the camera's wall clock was taken as UTC. |
| `offset` | Fixed offset such as `UTC+2`. |
| `iana` | IANA name such as `Europe/Berlin`, usually inferred from GPS. |
| `dst-mismatch` | With `--reference-zone`: a fixed offset that is the zone's other offset on that date (e.g. `UTC+2` in January for `Europe/Berlin`), i.e. the camera clock was not switched for DST. Add `--country` to avoid hits from trips abroad. |
| `iana-inconsistent` | IANA zone whose offset does not match `localDateTime − fileCreatedAt`. |

`tz-set --time-zone <IANA>` changes only assets whose time zone is null. For each one it computes the zone's offset in effect at the asset's local date and time (DST-aware) and sets `dateTimeOriginal` to the unchanged wall-clock time with that offset. The time shown in Immich stays the same; the absolute instant moves. Assets that already have a zone, or have no `dateTimeOriginal`, are skipped. Only IANA names are accepted; fixed offsets like `UTC+1` are rejected. Immich rewrites the sidecar and re-extracts metadata afterwards, so the stored zone ends up as the fixed offset (`UTC+1`/`UTC+2`), not the IANA name.

```sh
# Overview, with the DST check for cameras used in Germany
immich-admin cw tz-audit --reference-zone Europe/Berlin --country Germany

# List the assets without a time zone of one camera
immich-admin cw tz-audit --class none --make Canon

# Preview, then apply (stdin IDs need --yes)
immich-admin cw tz-audit --class none --make Canon -q | immich-admin cw tz-set --time-zone Europe/Berlin --ids-file - --dry-run
immich-admin cw tz-audit --class none --make Canon -q | immich-admin cw tz-set --time-zone Europe/Berlin --ids-file - --yes

# Or select by search directly: every asset without a time zone taken before 2010
immich-admin cw tz-set --time-zone Europe/Berlin --taken-before 2010-01-01T00:00:00Z
```

## Download or Mirror an Album

`download-album` resolves exactly one album by `--album-id` or `--album-name`, downloads its assets, and can optionally keep a local folder synchronized.

```sh
# Originals, one-time export
immich-admin cw download-album --album-name "2025-07-04 Garten" \
  --target-dir ./garten --size original

# Mirror small previews; preview local deletions first
immich-admin cw download-album --album-name "2025-07-04 Garten" \
  --target-dir ./garten --size thumbnail --ignore-videos --sync --dry-run
```

`--size` accepts `original`, `fullsize`, `preview` (the default), and `thumbnail`. Add `--timestamp-prefix` for chronologically sortable file names. In sync mode, a hidden `.immich-album-sync.json` manifest records files the tool created. Only manifest-tracked files can be removed, and the manifest is atomically updated after each completed item so interrupted runs can be resumed.

`--resize` converts image content to JPEG through ImageMagick. `--resize-video-preset 1080p-web-friendly` downloads videos as originals and transcodes them to compatible 1080p MP4 files with ffmpeg; it does not change photo behavior. Each tool is resolved from configuration, its corresponding environment variable, or `PATH` before the batch starts.

## Find Similar Assets

`find-similar` sends a local image to the read-only `immich-clip-probe` service and prints similar library assets. Configure `clip_probe.server` and `clip_probe.token` in the config file or through `IMMICH_CLIP_PROBE_SERVER` and `IMMICH_CLIP_PROBE_TOKEN`.

```sh
immich-admin cw find-similar DSC_2031.jpg
immich-admin cw find-similar DSC_2031.jpg --all --limit 5
immich-admin cw find-similar DSC_2031.jpg --ids-only
```

An empty result is normal. The default distance threshold is `0.01`; use `--all` when calibrating a suitable threshold.

## Search and Pipelines

The regular `search metadata` command accepts filename, asset type, camera, location, date, status, OCR, and description filters. `--all` follows every result page and `--ids-only` prints one UUID per line for pipes or `--ids-file` input.

```sh
# Save every image outside an album
immich-admin search metadata --is-not-in-album true --type IMAGE --all --ids-only > orphans.txt

# Inspect the first matching assets in PowerShell
immich-admin search metadata --original-file-name JPG --ids-only |
  Select-Object -First 2 |
  ForEach-Object { immich-admin assets info $_ }
```

For every command and flag, run `immich-admin <group> <command> --help`. For a machine-readable command reference, run `immich-admin return-agent-skill`.
