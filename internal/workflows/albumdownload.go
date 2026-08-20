// Package workflows: album-download workflow. Downloads every (optionally
// filtered) asset in one album to a local folder, either as a one-shot bulk
// download or, with --sync, as an ongoing mirror that also detects changes
// and removes local files for assets that left the album. See
// DownloadAlbum, PlanAlbumSync and ApplyAlbumSync.
package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// ManifestFileName is the hidden per-target-directory state file written by
// --sync mode (see Manifest). It never appears among the downloaded media
// files, and no other command reads or writes it.
const ManifestFileName = ".immich-album-sync.json"

const manifestVersion = 1

// Manifest tracks, for one target directory, the album and files
// SyncAlbum/ApplyAlbumSync downloaded there, so later runs can detect
// changes (re-download) and removals (delete locally) — and, just as
// importantly, so files not tracked here (anything the user placed in the
// folder themselves, or files from an unrelated album) are never touched.
type Manifest struct {
	Version   int                      `json:"version"`
	AlbumID   string                   `json:"albumId"`
	AlbumName string                   `json:"albumName"`
	Size      immichapi.AssetMediaSize `json:"size"`
	// Resize and TimestampPrefix record whether this target directory was
	// built with --resize / --timestamp-prefix, mirroring the Size guard:
	// both change the local file's identity (format/name) entirely, so a
	// later --sync run with a different setting is refused rather than
	// silently mixing conventions in one folder (see PlanAlbumSync).
	Resize          bool `json:"resize"`
	TimestampPrefix bool `json:"timestampPrefix"`
	// Assets is keyed by asset ID (string form of openapi_types.UUID).
	Assets map[string]ManifestAsset `json:"assets"`
}

// ManifestAsset is one asset SyncAlbum is tracking in a target directory.
type ManifestAsset struct {
	// FileName is the file's name (no directory) inside the target
	// directory, already including whatever extension was assigned at
	// download time (see AssignLocalNames and ExtensionForContentType).
	FileName string `json:"fileName"`
	// Checksum is the *original* asset's checksum (base64 SHA1) at the time
	// it was last downloaded, used for change detection even in --size
	// thumbnail mode: Immich exposes no separate thumbnail checksum, and a
	// thumbnail is derived deterministically from the original, so an
	// unchanged original checksum is treated as "nothing to refresh". A
	// metadata-only edit that doesn't change the original file's bytes
	// (e.g. a pure EXIF rotation) will therefore not trigger a thumbnail
	// re-download — a known, documented limitation.
	Checksum string `json:"checksum"`
	Type     string `json:"type"`
}

// DownloadAlbumOptions controls both DownloadAlbum (plain) and
// PlanAlbumSync/ApplyAlbumSync (--sync).
type DownloadAlbumOptions struct {
	// Size selects the media variant: immichapi.Original or
	// immichapi.Thumbnail (the command layer rejects any other
	// AssetMediaSize value before it reaches this package).
	Size immichapi.AssetMediaSize
	// IgnoreVideos drops every AssetTypeEnum VIDEO asset before planning or
	// downloading anything.
	IgnoreVideos bool
	// Resize, when Enabled, re-encodes every downloaded file to JPEG via
	// ImageMagick, optionally resizing it (see ResizeOptions).
	Resize ResizeOptions
	// TimestampPrefix prefixes each local file name with the asset's
	// capture date/time ("yyyy-MM-dd_HH_mm_ss", from LocalDateTime) so a
	// plain directory listing sorts chronologically.
	TimestampPrefix bool
	// DryRun previews the planned actions without downloading, deleting, or
	// writing the manifest.
	DryRun bool
}

// DefaultResizeQuality is the JPEG quality used by --resize when
// --resize-quality is not explicitly set.
const DefaultResizeQuality = 85

// ResizeOptions controls the optional ImageMagick post-processing step:
// every downloaded file (original or thumbnail) is re-encoded to JPEG,
// optionally resized to fit within Width/Height. It is a deliberate,
// documented exception to "always original format" for cases where local
// disk/transfer size matters more than preserving the exact source format.
type ResizeOptions struct {
	// Enabled turns the feature on. When false, every other field is
	// ignored and downloaded files keep their natural format.
	Enabled bool
	// Width and Height are the target bounding box in pixels; 0 means
	// unconstrained on that axis. ImageMagick's default -resize geometry
	// (WxH) fits the image within the box preserving aspect ratio; giving
	// only one of the two scales by that axis alone.
	Width, Height int
	// Quality is the JPEG quality (1-100). Zero is normalized to
	// DefaultResizeQuality by BuildImageMagickArgs.
	Quality int
	// ExecutablePath is the resolved ImageMagick binary ("magick" for v7+,
	// or the legacy "convert") — see ResolveImageMagickPath. Resolved once
	// by the command layer before a batch starts (fail fast), not by this
	// package.
	ExecutablePath string
}

// ResolveImageMagickPath returns the ImageMagick executable to invoke:
// explicit (from the config file's tools.imagemagick_path, or the
// IMMICH_IMAGEMAGICK_PATH env var — see internal/config) if non-empty,
// otherwise the first of "magick" (ImageMagick v7+, preferred) or "convert"
// (legacy v6) found on PATH. Meant to be called once before a download
// batch starts, so a missing tool fails fast rather than partway through.
func ResolveImageMagickPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	for _, name := range []string{"magick", "convert"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("ImageMagick executable not found: set tools.imagemagick_path in the config file (or the IMMICH_IMAGEMAGICK_PATH env var), or install ImageMagick so 'magick' or 'convert' is on PATH")
}

// resizeGeometry returns the ImageMagick -resize geometry string for
// width/height (see ResizeOptions.Width/Height), or "" if both are zero
// (meaning: re-encode/quality-adjust only, no resize).
func resizeGeometry(width, height int) string {
	switch {
	case width > 0 && height > 0:
		return fmt.Sprintf("%dx%d", width, height)
	case width > 0:
		return fmt.Sprintf("%dx", width)
	case height > 0:
		return fmt.Sprintf("x%d", height)
	default:
		return ""
	}
}

// BuildImageMagickArgs returns the CLI arguments (excluding the executable
// itself) to convert srcPath into dstPath as a JPEG per opts. Pure (no
// exec), so the exact geometry/quality construction is directly
// unit-testable. Works identically whether the resolved executable is the
// modern "magick" or the legacy "convert" — both accept
// "<input> [-resize GEOM] -quality Q <output>".
func BuildImageMagickArgs(srcPath, dstPath string, opts ResizeOptions) []string {
	args := []string{srcPath}
	if geometry := resizeGeometry(opts.Width, opts.Height); geometry != "" {
		args = append(args, "-resize", geometry)
	}
	quality := opts.Quality
	if quality <= 0 {
		quality = DefaultResizeQuality
	}
	args = append(args, "-quality", strconv.Itoa(quality), dstPath)
	return args
}

// RunImageMagickResize invokes opts.ExecutablePath to convert srcPath to
// dstPath (always JPEG) per opts. Stderr is captured and included in the
// error on failure for diagnosis.
func RunImageMagickResize(ctx context.Context, srcPath, dstPath string, opts ResizeOptions) error {
	args := BuildImageMagickArgs(srcPath, dstPath, opts)
	cmd := exec.CommandContext(ctx, opts.ExecutablePath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running %s %s: %w: %s", opts.ExecutablePath, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// FilterOutVideos returns the assets whose Type is not VIDEO, preserving
// order. Pure (no network) so it is unit-testable in isolation.
func FilterOutVideos(assets []immichapi.AssetResponseDto) []immichapi.AssetResponseDto {
	out := make([]immichapi.AssetResponseDto, 0, len(assets))
	for _, a := range assets {
		if a.Type != immichapi.VIDEO {
			out = append(out, a)
		}
	}
	return out
}

// timestampPrefixLayout is the Go reference-time layout for --timestamp-prefix,
// equivalent to the sortable "yyyy-MM-dd_HH_mm_ss" pattern.
const timestampPrefixLayout = "2006-01-02_15_04_05"

// AssignLocalNames computes a collision-safe local base file name (without
// extension) for every asset, derived from OriginalFileName and, if
// timestampPrefix, prefixed with the asset's capture date/time
// (LocalDateTime, formatted as timestampPrefixLayout) so a plain directory
// listing sorts chronologically. Immich allows duplicate original file
// names within one album (and a timestamp prefix does not fully rule out
// collisions either, e.g. burst shots within the same second); when two or
// more assets end up with the same final base name (case-insensitively),
// every colliding entry gets a short suffix built from its own asset ID
// appended, so the mapping is deterministic across runs regardless of slice
// order (assets are sorted by ID first).
func AssignLocalNames(assets []immichapi.AssetResponseDto, timestampPrefix bool) map[string]string {
	sorted := make([]immichapi.AssetResponseDto, len(assets))
	copy(sorted, assets)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Id.String() < sorted[j].Id.String() })

	localBase := func(a immichapi.AssetResponseDto) string {
		base := baseNameOf(a.OriginalFileName)
		if timestampPrefix {
			base = a.LocalDateTime.Format(timestampPrefixLayout) + "_" + base
		}
		return base
	}

	counts := map[string]int{}
	for _, a := range sorted {
		counts[strings.ToLower(localBase(a))]++
	}

	names := make(map[string]string, len(sorted))
	for _, a := range sorted {
		base := localBase(a)
		if counts[strings.ToLower(base)] > 1 {
			id := a.Id.String()
			suffix := id
			if len(id) > 8 {
				suffix = id[len(id)-8:]
			}
			base = base + "-" + suffix
		}
		names[a.Id.String()] = base
	}
	return names
}

// baseNameOf strips the extension from an original file name.
func baseNameOf(fileName string) string {
	return strings.TrimSuffix(fileName, filepath.Ext(fileName))
}

// LoadManifest reads ManifestFileName from targetDir. A missing file is not
// an error: it returns a zero-value Manifest (with an initialized Assets
// map) and existed=false, the normal state for a brand-new target
// directory or the first ever --sync run against it.
func LoadManifest(targetDir string) (m Manifest, existed bool, err error) {
	path := filepath.Join(targetDir, ManifestFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{Assets: map[string]ManifestAsset{}}, false, nil
	}
	if err != nil {
		return Manifest{}, false, fmt.Errorf("reading manifest %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, false, fmt.Errorf("parsing manifest %q: %w", path, err)
	}
	if m.Assets == nil {
		m.Assets = map[string]ManifestAsset{}
	}
	return m, true, nil
}

// SaveManifest writes m to ManifestFileName inside targetDir, overwriting
// any existing manifest.
func SaveManifest(targetDir string, m Manifest) error {
	m.Version = manifestVersion
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	path := filepath.Join(targetDir, ManifestFileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing manifest %q: %w", path, err)
	}
	return nil
}

// ResolveAlbum finds exactly one album given albumID (preferred, GET
// /albums/{id}) or, if albumID is nil, albumName (GET /albums?name=...).
// The caller (command layer) is responsible for ensuring exactly one of the
// two is provided. A name lookup errors if it matches zero or more than one
// album — Immich does not enforce unique album names — listing the matches'
// IDs so the caller can switch to --album-id.
func ResolveAlbum(ctx context.Context, c *client.Client, albumID *openapi_types.UUID, albumName string) (immichapi.AlbumResponseDto, error) {
	if albumID != nil {
		resp, err := c.API.GetAlbumInfoWithResponse(ctx, *albumID, &immichapi.GetAlbumInfoParams{})
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			return immichapi.AlbumResponseDto{}, fmt.Errorf("fetching album %s: %w", albumID.String(), err)
		}
		if resp.JSON200 == nil {
			return immichapi.AlbumResponseDto{}, fmt.Errorf("fetching album %s: response had no body", albumID.String())
		}
		return *resp.JSON200, nil
	}

	resp, err := c.API.GetAllAlbumsWithResponse(ctx, &immichapi.GetAllAlbumsParams{Name: &albumName})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return immichapi.AlbumResponseDto{}, fmt.Errorf("searching albums named %q: %w", albumName, err)
	}
	if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
		return immichapi.AlbumResponseDto{}, fmt.Errorf("no album named %q found", albumName)
	}
	matches := *resp.JSON200
	if len(matches) > 1 {
		ids := make([]string, len(matches))
		for i, a := range matches {
			ids[i] = a.Id.String()
		}
		return immichapi.AlbumResponseDto{}, fmt.Errorf("%d albums are named %q (%s) — use --album-id to disambiguate", len(matches), albumName, strings.Join(ids, ", "))
	}
	return matches[0], nil
}

// FetchFilteredAlbumAssets fetches every asset in albumID (paginated) and,
// if ignoreVideos, drops VIDEO assets.
func FetchFilteredAlbumAssets(ctx context.Context, c *client.Client, albumID openapi_types.UUID, ignoreVideos bool) ([]immichapi.AssetResponseDto, error) {
	assets, err := fetchAlbumAssets(ctx, c, albumID)
	if err != nil {
		return nil, err
	}
	if ignoreVideos {
		assets = FilterOutVideos(assets)
	}
	return assets, nil
}

// fetchAssetStream requests one asset (original or thumbnail, per size) and
// returns its body stream (caller must close it) plus the file extension to
// use: the original file's own extension for immichapi.Original, or sniffed
// from the actual response Content-Type for immichapi.Thumbnail (see
// ExtensionForContentType).
func fetchAssetStream(ctx context.Context, c *client.Client, a immichapi.AssetResponseDto, size immichapi.AssetMediaSize) (io.ReadCloser, string, error) {
	switch size {
	case immichapi.Original:
		resp, err := c.API.DownloadAsset(ctx, a.Id, nil)
		if err != nil {
			return nil, "", fmt.Errorf("downloading original: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return nil, "", fmt.Errorf("server returned %s (expected 200): %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return resp.Body, filepath.Ext(a.OriginalFileName), nil

	case immichapi.Thumbnail:
		thumbSize := immichapi.Thumbnail
		resp, err := c.API.ViewAsset(ctx, a.Id, &immichapi.ViewAssetParams{Size: &thumbSize})
		if err != nil {
			return nil, "", fmt.Errorf("downloading thumbnail: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return nil, "", fmt.Errorf("server returned %s (expected 200): %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return resp.Body, ExtensionForContentType(resp.Header.Get("Content-Type")), nil

	default:
		return nil, "", fmt.Errorf("unsupported download-album size %q (must be %q or %q)", size, immichapi.Original, immichapi.Thumbnail)
	}
}

// shouldResize reports whether downloadAssetFile should run ImageMagick
// against this asset's downloaded stream. GET /assets/{id}/thumbnail always
// returns a static preview image regardless of the asset's own Type (Immich
// generates an image thumbnail even for videos), so thumbnail-size resize
// is always safe when enabled. GET /assets/{id}/original returns the
// asset's real file, though — running ImageMagick against a video (or
// audio/other) original would treat it as a sequence of frames and either
// fail or silently produce one JPEG per frame, so original-size resize is
// restricted to Type == IMAGE. Pure (no network) so this decision is
// directly unit-testable.
func shouldResize(resize ResizeOptions, size immichapi.AssetMediaSize, assetType immichapi.AssetTypeEnum) bool {
	if !resize.Enabled {
		return false
	}
	if size == immichapi.Thumbnail {
		return true
	}
	return assetType == immichapi.IMAGE
}

// downloadAssetFile downloads one asset (original or thumbnail, per size)
// to destBasePath + <extension>, and returns the full destination path
// used. If shouldResize(resize, size, a.Type) is true, the downloaded file
// is first saved to a temporary path, then re-encoded to
// destBasePath+".jpg" via RunImageMagickResize, and the temporary file is
// removed — so the returned path is "destBasePath.jpg" in that case.
// Otherwise (resize disabled, or a non-image asset downloaded as
// --size original) the file is saved as-is in its natural format.
func downloadAssetFile(ctx context.Context, c *client.Client, a immichapi.AssetResponseDto, size immichapi.AssetMediaSize, destBasePath string, resize ResizeOptions) (string, error) {
	body, ext, err := fetchAssetStream(ctx, c, a, size)
	if err != nil {
		return "", err
	}
	defer body.Close()

	resizeThis := shouldResize(resize, size, a.Type)

	rawPath := destBasePath + ext
	if resizeThis {
		// Download to a distinct temporary path so a same-extension source
		// (e.g. an original that's already .jpg) never collides with the
		// final resized output path.
		rawPath = destBasePath + ".download-tmp" + ext
	}
	if err := writeAssetFile(rawPath, body); err != nil {
		return "", err
	}

	if !resizeThis {
		return rawPath, nil
	}

	finalPath := destBasePath + ".jpg"
	resizeErr := RunImageMagickResize(ctx, rawPath, finalPath, resize)
	os.Remove(rawPath) // always clean up the temporary raw download
	if resizeErr != nil {
		return "", fmt.Errorf("resizing with ImageMagick: %w", resizeErr)
	}
	return finalPath, nil
}

func writeAssetFile(path string, r io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating file %q: %w", path, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("writing file %q: %w", path, err)
	}
	return nil
}

// DownloadAlbum is the plain (non-sync) mode: it fetches, filters, and
// downloads every matching asset into targetDir, one HTTP request per
// asset, always overwriting any existing file of the same name. It builds
// no manifest and never deletes anything — reusable as a one-shot "get me
// this album's files" command. In dry-run mode it only prints the planned
// downloads.
func DownloadAlbum(ctx context.Context, c *client.Client, album immichapi.AlbumResponseDto, targetDir string, opts DownloadAlbumOptions) error {
	assets, err := FetchFilteredAlbumAssets(ctx, c, album.Id, opts.IgnoreVideos)
	if err != nil {
		return fmt.Errorf("fetching album assets: %w", err)
	}

	if opts.DryRun {
		fmt.Printf("[dry-run] download-album %q: would download %d asset(s) (size=%s) into %s\n", album.AlbumName, len(assets), opts.Size, targetDir)
		for _, a := range assets {
			fmt.Printf("[dry-run]   %s (%s)\n", a.OriginalFileName, a.Id)
		}
		return nil
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("creating target directory %q: %w", targetDir, err)
	}

	names := AssignLocalNames(assets, opts.TimestampPrefix)
	return RunBatch(assets,
		func(a immichapi.AssetResponseDto) string { return a.OriginalFileName },
		func(a immichapi.AssetResponseDto) error {
			base := filepath.Join(targetDir, names[a.Id.String()])
			dest, err := downloadAssetFile(ctx, c, a, opts.Size, base, opts.Resize)
			if err != nil {
				return err
			}
			fmt.Printf("Saved %s\n", dest)
			return nil
		},
	)
}

// SyncPlan is the result of classifying every filtered remote asset against
// a target directory's existing Manifest (see ComputeSyncPlan).
type SyncPlan struct {
	// Additions are assets not yet tracked in the manifest.
	Additions []immichapi.AssetResponseDto
	// Updates are tracked assets whose checksum has changed since the last
	// sync.
	Updates []immichapi.AssetResponseDto
	// Unchanged are tracked assets whose checksum is identical; nothing to
	// do for them.
	Unchanged []immichapi.AssetResponseDto
	// Removals are manifest entries whose asset ID is no longer present
	// among the (filtered) remote assets — the local file would be deleted.
	Removals []ManifestRemoval
}

// ManifestRemoval is one manifest entry ComputeSyncPlan found to no longer
// correspond to any (filtered) album asset.
type ManifestRemoval struct {
	AssetID  string
	FileName string
}

// ComputeSyncPlan classifies assets (already filtered, e.g. by
// FetchFilteredAlbumAssets) against manifest. Pure (no network, no
// filesystem access) so the decision logic is unit-testable directly.
func ComputeSyncPlan(assets []immichapi.AssetResponseDto, manifest Manifest) SyncPlan {
	var plan SyncPlan
	present := make(map[string]bool, len(assets))
	for _, a := range assets {
		id := a.Id.String()
		present[id] = true
		if prev, ok := manifest.Assets[id]; !ok {
			plan.Additions = append(plan.Additions, a)
		} else if prev.Checksum != a.Checksum {
			plan.Updates = append(plan.Updates, a)
		} else {
			plan.Unchanged = append(plan.Unchanged, a)
		}
	}

	var removedIDs []string
	for id := range manifest.Assets {
		if !present[id] {
			removedIDs = append(removedIDs, id)
		}
	}
	sort.Strings(removedIDs)
	for _, id := range removedIDs {
		plan.Removals = append(plan.Removals, ManifestRemoval{AssetID: id, FileName: manifest.Assets[id].FileName})
	}
	return plan
}

// PlanAlbumSync is the read-only "getting the information" phase of --sync:
// it loads the target directory's existing manifest (if any), fetches and
// filters the album's current assets, and classifies them with
// ComputeSyncPlan. It performs no writes and is used both for --dry-run and
// as the first phase of a real sync run (see ApplyAlbumSync).
//
// It refuses to proceed if an existing manifest names a different album, or
// was built with a different --size, than the current run — either would
// make removal detection unsafe or nonsensical; the caller should point
// --target-dir at a fresh folder instead.
func PlanAlbumSync(ctx context.Context, c *client.Client, album immichapi.AlbumResponseDto, targetDir string, opts DownloadAlbumOptions) ([]immichapi.AssetResponseDto, SyncPlan, Manifest, error) {
	manifest, existed, err := LoadManifest(targetDir)
	if err != nil {
		return nil, SyncPlan{}, Manifest{}, err
	}
	if existed {
		if manifest.AlbumID != "" && manifest.AlbumID != album.Id.String() {
			return nil, SyncPlan{}, Manifest{}, fmt.Errorf("manifest in %q tracks album %q (%s), not %q (%s) — use a different --target-dir or matching --album-id/--album-name", targetDir, manifest.AlbumName, manifest.AlbumID, album.AlbumName, album.Id.String())
		}
		if manifest.Size != "" && manifest.Size != opts.Size {
			return nil, SyncPlan{}, Manifest{}, fmt.Errorf("manifest in %q was created with --size %s, not %s — use a different --target-dir to switch", targetDir, manifest.Size, opts.Size)
		}
		if manifest.Resize != opts.Resize.Enabled {
			return nil, SyncPlan{}, Manifest{}, fmt.Errorf("manifest in %q was created with --resize=%t, not %t — use a different --target-dir to switch", targetDir, manifest.Resize, opts.Resize.Enabled)
		}
		if manifest.TimestampPrefix != opts.TimestampPrefix {
			return nil, SyncPlan{}, Manifest{}, fmt.Errorf("manifest in %q was created with --timestamp-prefix=%t, not %t — use a different --target-dir to switch", targetDir, manifest.TimestampPrefix, opts.TimestampPrefix)
		}
	}

	assets, err := FetchFilteredAlbumAssets(ctx, c, album.Id, opts.IgnoreVideos)
	if err != nil {
		return nil, SyncPlan{}, Manifest{}, fmt.Errorf("fetching album assets: %w", err)
	}

	return assets, ComputeSyncPlan(assets, manifest), manifest, nil
}

// ApplyAlbumSync executes plan against targetDir: downloads every asset in
// plan.Additions and plan.Updates, deletes the local file for every
// plan.Removals entry, and persists the updated manifest (stamping it with
// album/opts.Size so future runs can validate against it). Downloads
// continue on a per-asset error and are summarized at the end (the usual
// bulk convention), but a local-file deletion error aborts immediately —
// leaving the manifest unsaved is safer than saving one that no longer
// matches disk state.
func ApplyAlbumSync(ctx context.Context, c *client.Client, album immichapi.AlbumResponseDto, targetDir string, allAssets []immichapi.AssetResponseDto, plan SyncPlan, manifest Manifest, opts DownloadAlbumOptions) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("creating target directory %q: %w", targetDir, err)
	}
	if manifest.Assets == nil {
		manifest.Assets = map[string]ManifestAsset{}
	}
	manifest.AlbumID = album.Id.String()
	manifest.AlbumName = album.AlbumName
	manifest.Size = opts.Size
	manifest.Resize = opts.Resize.Enabled
	manifest.TimestampPrefix = opts.TimestampPrefix

	for _, r := range plan.Removals {
		path := filepath.Join(targetDir, r.FileName)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("deleting %q: %w", path, err)
		}
		delete(manifest.Assets, r.AssetID)
		fmt.Printf("Removed %s (asset no longer in album)\n", path)
	}

	names := AssignLocalNames(allAssets, opts.TimestampPrefix)
	toDownload := make([]immichapi.AssetResponseDto, 0, len(plan.Additions)+len(plan.Updates))
	toDownload = append(toDownload, plan.Additions...)
	toDownload = append(toDownload, plan.Updates...)

	downloadErr := RunBatch(toDownload,
		func(a immichapi.AssetResponseDto) string { return a.OriginalFileName },
		func(a immichapi.AssetResponseDto) error {
			base := filepath.Join(targetDir, names[a.Id.String()])
			dest, err := downloadAssetFile(ctx, c, a, opts.Size, base, opts.Resize)
			if err != nil {
				return err
			}
			manifest.Assets[a.Id.String()] = ManifestAsset{
				FileName: filepath.Base(dest),
				Checksum: a.Checksum,
				Type:     string(a.Type),
			}
			fmt.Printf("Saved %s\n", dest)
			return nil
		},
	)

	if err := SaveManifest(targetDir, manifest); err != nil {
		return err
	}
	return downloadErr
}
