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
- **Albums**: `client-workflow download-album --album-name NAME --target-dir
  DIR --size original --sync` mirrors an album (manifest-tracked, safe to
  resume); `--size preview` + `--resize`/`--resize-video-preset` for small
  shareable copies. `fix-album-dates` reconciles date-named albums
  (report-only for year albums). `add-users-to-album-with-pattern` bulk-shares.
- **Tags**: `tags upsert`, `tags bulk-tag`, `tags tag` for assignment;
  `client-workflow tag-delete --include/--exclude` for regex bulk deletion
  (permanent — dry-run first).
- **Upload**: `assets upload FILE...` (timestamps default to file mtime;
  `--sidecar`/`--filename` are single-file only).
