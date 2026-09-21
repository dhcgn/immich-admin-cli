# immich-admin-cli

[![Build](https://img.shields.io/github/actions/workflow/status/dhcgn/immich-admin-cli/ci.yml?branch=main)](https://github.com/dhcgn/immich-admin-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/dhcgn/immich-admin-cli)](https://github.com/dhcgn/immich-admin-cli/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/dhcgn/immich-admin-cli)](go.mod)
[![Downloads](https://img.shields.io/github/downloads/dhcgn/immich-admin-cli/total)](https://github.com/dhcgn/immich-admin-cli/releases)
[![License](https://img.shields.io/github/license/dhcgn/immich-admin-cli)](LICENSE)

> [!WARNING]
> **No warranty. Use at your own risk.** This tool performs bulk and destructive operations directly against your Immich server. A wrong flag, unexpected server response, or bug can cause permanent data loss. Keep a verified backup, test with non-critical data, and use `--dry-run` before changing anything. This project is not affiliated with Immich.

## What You Can Do

| Area | Capabilities | Start here |
|------|--------------|------------|
| Asset management | Inspect, upload, download originals or thumbnails, verify remote files, copy metadata, update metadata, and trash or permanently delete assets | `assets --help` |
| Albums and sharing | Create, rename, delete, and manage album assets; add users to matching albums in bulk; merge albums; download or mirror an album locally | `albums --help`, `cw add-users-to-album-with-pattern`, `cw merge-album`, `cw download-album` |
| Find and organize | Search by filename, type, camera, location, date, status, OCR, or description; emit pipeable asset IDs; manage tags and tag assignments | `search metadata --help`, `tags --help` |
| Repair and replace | Replace an asset while preserving its Immich metadata; locate missing thumbnails; repair malformed JPEG/TIFF files; remove Google Takeout JSON sidecars | `cw replace-asset`, `cw find-no-thumbhash`, `cw repair-assets` |
| Date and duplicate assistance | Check and correct assets that do not fit date-named albums; find visually similar library assets for a local image | `cw fix-album-dates`, `cw find-similar` |
| Watch folders | Watch local folders and upload new files, or download matching assets as they appear | `cw watch-upload`, `cw watch-download` |
| Server workflows and users | List, inspect, and read logs for Immich server-side workflows; look up users and the authenticated account | `immich-workflow --help`, `users --help` |
| Automation | Preview changes with `--dry-run`, bypass prompts with `--yes`, pass IDs through files or stdin, use JSON/IDs-only output, and retrieve agent instructions from the binary | `immich-admin return-agent-skill` |

Run `immich-admin --help` for the complete command tree.

## Use With an AI Agent

Give an agent such as Hermes this prompt:

```text
Download the latest release of immich-admin-cli from https://github.com/dhcgn/immich-admin-cli/releases and make the binary available on PATH.

Before using it, run `immich-admin update --check`; install an available update before performing work. Then run `immich-admin return-agent-skill`, read the generated instructions, and save them as your persistent skill for future immich-admin-cli tasks.

Use the skill and the CLI's --help output as the source of truth for commands and flags. Use `--json` when you need more complete, structured information than the human-readable output provides. For every operation that can change Immich data or local files, run it with --dry-run first, show the planned effect, and ask for confirmation before using --yes or --force.

Keep the Immich server up to date. immich-admin-cli follows the current Immich API specification, so an outdated server can lack commands or behavior expected by a newer CLI release.
```

## In-Depth Guide

Read [Client Workflow Guide](docs/client-workflows.md) for workflow safety, repair criteria, album synchronization behavior, external-tool requirements, and copyable command examples.

## Motivation

immich-admin-cli provides command-line and automation support for bulk Immich tasks that are not available in the web interface, including conversion, repair, and deletion of photo assets.

The generated agent skill ([immich-admin-cli/SKILL.md](immich-admin-cli/SKILL.md)) describes every command for AI agents. The CLI also supports machine-readable `--json` output, pipeable `--ids-only` IDs, `--dry-run` previews, and non-interactive `--yes` flags. Retrieve the current skill from the binary with `immich-admin return-agent-skill`.

## Self-Update

`immich-admin update` checks the latest [GitHub release](https://github.com/dhcgn/immich-admin-cli/releases) for a build matching the current OS/architecture and, on confirmation, replaces the running executable in place and restarts it via [`gh-update`](https://github.com/dhcgn/gh-update).

```sh
immich-admin update            # check, prompt, then install
immich-admin update --check    # only check, don't install
immich-admin update --yes      # install without prompting
```

Release binaries are named `immich-admin_<os>_<arch>[.exe]` without a version number so self-update can replace the running file. Use `immich-admin --version` or the release tag to see the installed version.

## API Coverage

<!-- Generated by tools/apitable — do not edit between the markers. Refresh with `go generate ./...` -->
<!-- API-TABLE:BEGIN -->
**28 of 250 endpoints implemented** (24 deprecated and 2 internal endpoints omitted per project policy).

<details>
<summary><b>API keys</b> (0/6)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/api-keys` | `getApiKeys` | Stable |
|  | POST | `/api-keys` | `createApiKey` | Stable |
|  | GET | `/api-keys/me` | `getMyApiKey` | Stable |
|  | DELETE | `/api-keys/{id}` | `deleteApiKey` | Stable |
|  | GET | `/api-keys/{id}` | `getApiKey` | Stable |
|  | POST | `/api-keys/{id}/rotate` | `rotateApiKey` | – |

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
<summary><b>Albums</b> (8/13)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
| ✅ | GET | `/albums` | `getAllAlbums` | Stable |
| ✅ | POST | `/albums` | `createAlbum` | Stable |
|  | PUT | `/albums/assets` | `addAssetsToAlbums` | Stable |
|  | GET | `/albums/statistics` | `getAlbumStatistics` | Stable |
| ✅ | DELETE | `/albums/{id}` | `deleteAlbum` | Stable |
| ✅ | GET | `/albums/{id}` | `getAlbumInfo` | Stable |
| ✅ | PATCH | `/albums/{id}` | `updateAlbumInfo` | Stable |
| ✅ | DELETE | `/albums/{id}/assets` | `removeAssetFromAlbum` | Stable |
| ✅ | PUT | `/albums/{id}/assets` | `addAssetsToAlbum` | Stable |
|  | GET | `/albums/{id}/map-markers` | `getAlbumMapMarkers` | – |
|  | DELETE | `/albums/{id}/user/{userId}` | `removeUserFromAlbum` | Stable |
|  | PUT | `/albums/{id}/user/{userId}` | `updateAlbumUser` | Stable |
| ✅ | PUT | `/albums/{id}/users` | `addUsersToAlbum` | Stable |

</details>

<details>
<summary><b>Asset files</b> (0/4)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/asset-files` | `searchAssetFiles` | Alpha |
|  | DELETE | `/asset-files/{id}` | `deleteAssetFile` | Alpha |
|  | GET | `/asset-files/{id}` | `getAssetFile` | Alpha |
|  | GET | `/asset-files/{id}/download` | `downloadAssetFile` | Alpha |

</details>

<details>
<summary><b>Assets</b> (7/24)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
| ✅ | DELETE | `/assets` | `deleteAssets` | Stable |
| ✅ | POST | `/assets` | `uploadAsset` | Stable |
| ✅ | POST | `/assets/bulk-upload-check` | `checkBulkUpload` | Stable |
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
<summary><b>Cluster groups</b> (0/8)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/cluster-groups/requests` | `getClusterGroupRequests` | – |
|  | DELETE | `/cluster-groups/requests/{id}` | `deleteClusterGroupRequest` | – |
|  | POST | `/cluster-groups/requests/{id}/accept` | `acceptClusterGroupRequest` | – |
|  | POST | `/cluster-groups/{id}/leave` | `leaveClusterGroup` | – |
|  | POST | `/cluster-groups/{id}/regenerate-people` | `clusterGroupRegeneratePeople` | – |
|  | GET | `/cluster-groups/{id}/requests` | `getClusterGroupRequestsForGroup` | – |
|  | PUT | `/cluster-groups/{id}/requests` | `createClusterGroupRequest` | – |
|  | GET | `/cluster-groups/{id}/users` | `getClusterGroupUsers` | – |

</details>

<details>
<summary><b>Config (admin)</b> (0/3)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/admin/config` | `getAdminConfig` | Alpha |
|  | PUT | `/admin/config` | `updateAdminConfig` | Alpha |
|  | GET | `/admin/config/defaults` | `getAdminConfigDefaults` | Alpha |

</details>

<details>
<summary><b>Config (public)</b> (0/2)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/public/config` | `getPublicConfig` | Alpha |
|  | GET | `/public/config/defaults` | `getPublicConfigDefaults` | Alpha |

</details>

<details>
<summary><b>Config (user)</b> (0/2)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/config` | `getUserConfig` | Alpha |
|  | GET | `/config/defaults` | `getUserConfigDefaults` | Alpha |

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
|  | POST | `/people/merge` | `mergePeople` | Stable |
|  | DELETE | `/people/{id}` | `deletePerson` | Stable |
|  | GET | `/people/{id}` | `getPerson` | Stable |
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
<summary><b>Search</b> (1/9)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/search/cities` | `getAssetsByCity` | Stable |
|  | GET | `/search/explore` | `getExploreData` | Stable |
| ✅ | POST | `/search/metadata` | `searchAssets` | Stable |
|  | GET | `/search/person` | `searchPerson` | Stable |
|  | GET | `/search/places` | `searchPlaces` | Stable |
|  | POST | `/search/random` | `searchRandom` | Stable |
|  | POST | `/search/smart` | `searchSmart` | Stable |
|  | POST | `/search/statistics` | `searchAssetStatistics` | Stable |
|  | GET | `/search/suggestions` | `getSearchSuggestions` | Stable |

</details>

<details>
<summary><b>Server</b> (0/12)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
|  | GET | `/server/about` | `getAboutInfo` | Stable |
|  | GET | `/server/apk-links` | `getApkLinks` | Stable |
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
<summary><b>System config</b> (0/1)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
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
<summary><b>Workflows</b> (3/7)</summary>

| Impl | Method | Path | Operation | State |
|:----:|--------|------|-----------|-------|
| ✅ | GET | `/workflows` | `searchWorkflows` | – |
|  | POST | `/workflows` | `createWorkflow` | – |
|  | GET | `/workflows/triggers` | `getWorkflowTriggers` | – |
|  | DELETE | `/workflows/{id}` | `deleteWorkflow` | – |
| ✅ | GET | `/workflows/{id}` | `getWorkflow` | – |
| ✅ | GET | `/workflows/{id}/logs` | `getWorkflowLogs` | – |
|  | GET | `/workflows/{id}/share` | `getWorkflowForShare` | – |

</details>
<!-- API-TABLE:END -->
