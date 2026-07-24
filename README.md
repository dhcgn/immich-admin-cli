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

## Client Workflows

The main purpose of this tool: **client workflows** are multi-step orchestrations that combine several Immich API calls with local processing (e.g. re-encoding) into one command. They run entirely on the client — not to be confused with Immich's server-side Workflows API.

All workflows follow the same safety model: `--dry-run` shows what would happen without changing anything, the destructive final step requires `--yes`, originals are only removed **after** the replacement has been uploaded and verified, and removal goes to the trash (restorable) by default.

| Status | Command | Description |
|:------:|---------|-------------|
| ✅ done | `client-workflow replace-asset` | Replace an existing asset with a new file, keeping its metadata |
| ✅ done | `client-workflow tag-delete` | Delete tags whose full path matches an include/exclude regex |
| ✅ done | `client-workflow find-no-thumbhash` | Find assets without a thumbhash (likely corrupt or unprocessed) |
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

Flags: `--type TYPE` (pre-filter: IMAGE, VIDEO, …), `--original-file-name NAME` (substring match), `--page-size N` (default 250, max 1000), `--json`, `--ids-only` / `-q`.

Typical follow-up — save corrupt IDs, then use them later with a repair workflow:

```sh
# Save corrupt asset IDs to a file
immich-admin cw find-no-thumbhash --type IMAGE -q > corrupt-ids.txt

# Inspect each one
cat corrupt-ids.txt | ForEach-Object { immich-admin assets info $_ }
```

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

## API Coverage

<!-- Generated by tools/apitable — do not edit between the markers. Refresh with `go generate ./...` -->
<!-- API-TABLE:BEGIN -->
**10 of 235 endpoints implemented** (17 deprecated and 2 internal endpoints omitted per project policy).

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
<summary><b>Albums</b> (0/13)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/albums` | `getAllAlbums` | Stable |
|  | POST | `/albums` | `createAlbum` | Stable |
|  | PUT | `/albums/assets` | `addAssetsToAlbums` | Stable |
|  | GET | `/albums/statistics` | `getAlbumStatistics` | Stable |
|  | DELETE | `/albums/{id}` | `deleteAlbum` | Stable |
|  | GET | `/albums/{id}` | `getAlbumInfo` | Stable |
|  | PATCH | `/albums/{id}` | `updateAlbumInfo` | Stable |
|  | DELETE | `/albums/{id}/assets` | `removeAssetFromAlbum` | Stable |
|  | PUT | `/albums/{id}/assets` | `addAssetsToAlbum` | Stable |
|  | GET | `/albums/{id}/map-markers` | `getAlbumMapMarkers` | – |
|  | DELETE | `/albums/{id}/user/{userId}` | `removeUserFromAlbum` | Stable |
|  | PUT | `/albums/{id}/user/{userId}` | `updateAlbumUser` | Stable |
|  | PUT | `/albums/{id}/users` | `addUsersToAlbum` | Stable |

</details>

<details>
<summary><b>Assets</b> (5/24)</summary>

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
|  | GET | `/assets/{id}/thumbnail` | `viewAsset` | Stable |
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
<summary><b>Tags</b> (3/8)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
| ✅ | GET | `/tags` | `getAllTags` | Stable |
|  | POST | `/tags` | `createTag` | Stable |
|  | PUT | `/tags` | `upsertTags` | Stable |
|  | PUT | `/tags/assets` | `bulkTagAssets` | Stable |
| ✅ | DELETE | `/tags/{id}` | `deleteTag` | Stable |
| ✅ | GET | `/tags/{id}` | `getTagById` | Stable |
|  | DELETE | `/tags/{id}/assets` | `untagAssets` | Stable |
|  | PUT | `/tags/{id}/assets` | `tagAssets` | Stable |

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
<summary><b>Users</b> (1/14)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/users` | `searchUsers` | Stable |
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
|  | GET | `/users/{id}` | `getUser` | Stable |
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

