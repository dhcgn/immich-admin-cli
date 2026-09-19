package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// Watch state + upload orchestration (local -> Immich).

const watchUploadStateFile = ".immich-watch-upload.json"

// DefaultWatchUploadTagPattern tags flat uploads with the upload day.
const DefaultWatchUploadTagPattern = "immich-admin-cli/watch/{yyyy-MM-dd}"

// WatchUploadOptions controls watch-upload.
type WatchUploadOptions struct {
	WatchDir   string
	Mode       string // flat | by-subfolder
	Depth      int
	TagPattern string
	AlbumID    *openapi_types.UUID
	AlbumName  string
	Interval   time.Duration
	StableFor  time.Duration
	Once       bool
	DryRun     bool
	Yes        bool
	Quiet      bool
	JSON       bool
}

// WatchUploadStats is the per-interval summary.
type WatchUploadStats struct {
	Uploaded        int `json:"uploaded"`
	LinkedDuplicate int `json:"linkedDuplicate"`
	SkippedUnstable int `json:"skippedUnstable"`
	Failed          int `json:"failed"`
	SkippedDone     int `json:"-"`
}

// WatchFile is one candidate local file.
type WatchFile struct {
	AbsPath   string
	Rel       string
	Subfolder string
	Size      int64
	ModTime   time.Time
}

type watchUploadEntry struct {
	Checksum  string `json:"checksum,omitempty"`
	AssetID   string `json:"assetId,omitempty"`
	Size      int64  `json:"size"`
	MtimeUnix int64  `json:"mtimeUnix"`
	FirstSeen int64  `json:"firstSeenUnix"`
}

// RenderTagPattern replaces {yyyy-MM-dd} with t's date. Only that
// placeholder exists in v1; any other {…} is an error. Pure for testing.
func RenderTagPattern(pattern string, t time.Time) (string, error) {
	if pattern == "" {
		return "", fmt.Errorf("tag pattern must not be empty")
	}
	out := strings.ReplaceAll(pattern, "{yyyy-MM-dd}", t.Format("2006-01-02"))
	if strings.Contains(out, "{") {
		return "", fmt.Errorf("unknown placeholder in --tag-pattern %q: only {yyyy-MM-dd} is supported", pattern)
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("tag pattern %q renders empty", pattern)
	}
	return out, nil
}

// ScanWatchDir lists candidate files per mode (depth 1 only in v1).
// flat: top-level files. by-subfolder: immediate subdir files only.
func ScanWatchDir(watchDir, mode string, depth int) ([]WatchFile, error) {
	if depth != 1 {
		return nil, fmt.Errorf("only --depth 1 is supported in v1 (got %d)", depth)
	}
	entries, err := os.ReadDir(watchDir)
	if err != nil {
		return nil, fmt.Errorf("reading watch dir %q: %w", watchDir, err)
	}
	var out []WatchFile
	addFile := func(abs, rel, sub string) {
		fi, err := os.Lstat(abs)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
			return
		}
		base := filepath.Base(abs)
		if strings.HasPrefix(base, ".") || base == watchUploadStateFile {
			return
		}
		out = append(out, WatchFile{AbsPath: abs, Rel: rel, Subfolder: sub, Size: fi.Size(), ModTime: fi.ModTime()})
	}
	switch mode {
	case "flat":
		for _, e := range entries {
			if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
				continue
			}
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			addFile(filepath.Join(watchDir, e.Name()), e.Name(), "")
		}
	case "by-subfolder":
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Type()&os.ModeSymlink != 0 {
				continue
			}
			sub := e.Name()
			subEntries, err := os.ReadDir(filepath.Join(watchDir, sub))
			if err != nil {
				continue
			}
			for _, f := range subEntries {
				if f.IsDir() || f.Type()&os.ModeSymlink != 0 || strings.HasPrefix(f.Name(), ".") {
					continue
				}
				addFile(filepath.Join(watchDir, sub, f.Name()), filepath.Join(sub, f.Name()), sub)
			}
		}
	default:
		return nil, fmt.Errorf("invalid --mode %q: must be flat or by-subfolder", mode)
	}
	return out, nil
}

func loadWatchUploadState(watchDir string) (map[string]watchUploadEntry, error) {
	path := filepath.Join(watchDir, watchUploadStateFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]watchUploadEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state %q: %w", path, err)
	}
	var st map[string]watchUploadEntry
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parsing state %q: %w", path, err)
	}
	if st == nil {
		st = map[string]watchUploadEntry{}
	}
	return st, nil
}

func saveWatchUploadState(watchDir string, st map[string]watchUploadEntry) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding watch state: %w", err)
	}
	path := filepath.Join(watchDir, watchUploadStateFile)
	tmp, err := os.CreateTemp(watchDir, watchUploadStateFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp state file: %w", err)
	}
	tmpPath := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmpPath)
		if werr != nil {
			return fmt.Errorf("writing state %q: %w", path, werr)
		}
		return fmt.Errorf("writing state %q: %w", path, cerr)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("writing state %q: %w", path, err)
	}
	return nil
}

// stableForGate reports whether the file has been unchanged for stableFor.
// It updates st[rel] in place (first-seen tracking). stableFor <= 0 means
// no gate (always stable).
func stableForGate(st map[string]watchUploadEntry, rel string, size int64, mtime time.Time, now time.Time, stableFor time.Duration) bool {
	if stableFor <= 0 {
		return true
	}
	e, ok := st[rel]
	mtimeUnix := mtime.Unix()
	if !ok || e.Size != size || e.MtimeUnix != mtimeUnix {
		st[rel] = watchUploadEntry{Checksum: e.Checksum, AssetID: e.AssetID, Size: size, MtimeUnix: mtimeUnix, FirstSeen: now.Unix()}
		return false
	}
	return now.Unix()-e.FirstSeen >= int64(stableFor.Seconds())
}

// ResolveOrCreateAlbumByName finds an album by exact name or creates it.
// dryRun prints instead of creating; yes skips the creation prompt.
func ResolveOrCreateAlbumByName(ctx context.Context, c *client.Client, name string, dryRun, yes bool) (immichapi.AlbumResponseDto, error) {
	if album, err := ResolveAlbum(ctx, c, nil, name); err == nil {
		return album, nil
	}
	if dryRun {
		fmt.Printf("[dry-run] would create album %q\n", name)
		return immichapi.AlbumResponseDto{AlbumName: name}, nil
	}
	if !yes {
		fmt.Printf("Create album %q? [y/N]: ", name)
		var answer string
		fmt.Scanln(&answer)
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			return immichapi.AlbumResponseDto{}, fmt.Errorf("album %q does not exist (creation declined)", name)
		}
	}
	resp, err := c.API.CreateAlbumWithResponse(ctx, immichapi.CreateAlbumDto{AlbumName: name})
	if err != nil {
		return immichapi.AlbumResponseDto{}, fmt.Errorf("creating album: %w", err)
	}
	if resp.StatusCode() != 201 {
		return immichapi.AlbumResponseDto{}, fmt.Errorf("creating album: server returned %s", resp.Status())
	}
	if resp.JSON201 == nil {
		return immichapi.AlbumResponseDto{}, fmt.Errorf("creating album: response had no body")
	}
	return *resp.JSON201, nil
}

// ensureWatchTag returns the tag ID for value, creating it unless dryRun.
// yes skips the creation prompt.
func ensureWatchTag(ctx context.Context, c *client.Client, value string, dryRun, yes bool) (openapi_types.UUID, error) {
	if t, err := ResolveTagByValue(ctx, c, value); err == nil {
		return t.Id, nil
	}
	if dryRun {
		fmt.Printf("[dry-run] would create tag %q\n", value)
		return openapi_types.UUID{}, nil
	}
	if !yes {
		fmt.Printf("Create tag %q? [y/N]: ", value)
		var answer string
		fmt.Scanln(&answer)
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			return openapi_types.UUID{}, fmt.Errorf("tag %q does not exist (creation declined)", value)
		}
	}
	return ResolveOrCreateTag(ctx, c, value)
}

// RunWatchUploadOnce performs one watch-upload scan.
func RunWatchUploadOnce(ctx context.Context, c *client.Client, opts WatchUploadOptions) (WatchUploadStats, error) {
	var stats WatchUploadStats
	now := time.Now()

	files, err := ScanWatchDir(opts.WatchDir, opts.Mode, opts.Depth)
	if err != nil {
		return stats, err
	}

	st, err := loadWatchUploadState(opts.WatchDir)
	if err != nil {
		return stats, err
	}
	stateDirty := false

	// Stability gate first (cheap: size+mtime only).
	type pending struct {
		f        WatchFile
		tagValue string
		checksum string
	}
	var stables []pending
	tagValues := map[string]bool{}
	for _, f := range files {
		// Already completed and unchanged: skip without re-hashing.
		if e, ok := st[f.Rel]; ok && e.AssetID != "" && e.Checksum != "" &&
			e.Size == f.Size && e.MtimeUnix == f.ModTime.Unix() {
			stats.SkippedDone++
			continue
		}
		if !stableForGate(st, f.Rel, f.Size, f.ModTime, now, opts.StableFor) {
			stats.SkippedUnstable++
			stateDirty = true
			continue
		}
		stateDirty = true
		var tv string
		if opts.Mode == "flat" {
			tv, err = RenderTagPattern(opts.TagPattern, now)
			if err != nil {
				return stats, err
			}
		} else {
			tv = f.Subfolder
		}
		tagValues[tv] = true
		// Reuse cached checksum when size+mtime match to avoid re-hashing videos.
		cs := ""
		if e, ok := st[f.Rel]; ok && e.Size == f.Size && e.MtimeUnix == f.ModTime.Unix() {
			cs = e.Checksum
		}
		stables = append(stables, pending{f: f, tagValue: tv, checksum: cs})
	}

	if len(stables) == 0 {
		if stateDirty && !opts.DryRun {
			_ = saveWatchUploadState(opts.WatchDir, st)
		}
		return stats, nil
	}

	// Hash stable files (unless cached).
	checksums := make(map[string]string, len(stables))
	var toCheck []string
	var relOf = make(map[string]string) // abs -> rel
	for _, p := range stables {
		relOf[p.f.AbsPath] = p.f.Rel
		if p.checksum != "" {
			checksums[p.f.AbsPath] = p.checksum
			toCheck = append(toCheck, p.f.AbsPath)
			continue
		}
		sum, herr := fileSHA1Base64(p.f.AbsPath)
		if herr != nil {
			fmt.Fprintf(os.Stderr, "Error: file %s: %v\n", p.f.AbsPath, herr)
			stats.Failed++
			continue
		}
		checksums[p.f.AbsPath] = sum
		toCheck = append(toCheck, p.f.AbsPath)
		e := st[p.f.Rel]
		e.Checksum, e.Size, e.MtimeUnix = sum, p.f.Size, p.f.ModTime.Unix()
		st[p.f.Rel] = e
	}

	// Server is source of truth: bulk-upload-check first.
	results := map[string]BulkCheckEntry{}
	if len(toCheck) > 0 && !opts.DryRun {
		entries, err := CheckBulkUploadChecksums(ctx, c, toCheck, checksums)
		if err != nil {
			return stats, err
		}
		for _, e := range entries {
			results[e.File] = e
		}
	}

	// Resolve tags (and optional album) unless dry-run only prints.
	tagIDs := map[string]openapi_types.UUID{}
	var albumID *openapi_types.UUID
	if !opts.DryRun {
		for tv := range tagValues {
			id, err := ensureWatchTag(ctx, c, tv, false, opts.Yes)
			if err != nil {
				return stats, err
			}
			tagIDs[tv] = id
		}
		if opts.AlbumID != nil {
			album, err := ResolveAlbum(ctx, c, opts.AlbumID, "")
			if err != nil {
				return stats, err
			}
			albumID = &album.Id
		} else if opts.AlbumName != "" {
			album, err := ResolveOrCreateAlbumByName(ctx, c, opts.AlbumName, false, opts.Yes)
			if err != nil {
				return stats, err
			}
			albumID = &album.Id
		}
	}

	quiet := opts.Quiet || opts.JSON
	// Group links per tag/album for fewer requests.
	tagGroups := map[string][]openapi_types.UUID{}
	var albumIDs []openapi_types.UUID

	for i, p := range stables {
		if _, ok := checksums[p.f.AbsPath]; !ok {
			continue // hashing failed already counted
		}
		if opts.DryRun {
			tv := p.tagValue
			if opts.Mode == "flat" {
				tv, _ = RenderTagPattern(opts.TagPattern, now)
			}
			fmt.Printf("[dry-run] would upload %s (tag %q)\n", p.f.AbsPath, tv)
			continue
		}
		r, ok := results[p.f.AbsPath]
		if !ok {
			stats.Failed++
			continue
		}
		var assetID openapi_types.UUID
		switch r.Status {
		case BulkCheckMissing:
			prog := NewByteProgress(p.f.AbsPath, p.f.Size, i+1, len(stables), quiet)
			id, uerr := UploadAssetFile(ctx, c, p.f.AbsPath, UploadOptions{Checksum: &r.Checksum, Progress: prog})
			prog.Finish()
			if uerr != nil {
				var dup *DuplicateUploadError
				if errors.As(uerr, &dup) && dup.HasID {
					assetID = dup.ExistingID
					stats.LinkedDuplicate++
				} else {
					fmt.Fprintf(os.Stderr, "Error: file %s: %v\n", p.f.AbsPath, uerr)
					stats.Failed++
					continue
				}
			} else {
				assetID = id
				stats.Uploaded++
			}
		case BulkCheckUploaded:
			assetID = *r.AssetID
			stats.LinkedDuplicate++
		default:
			fmt.Fprintf(os.Stderr, "Error: file %s: unsupported format (server has no assetId)\n", p.f.AbsPath)
			stats.Failed++
			continue
		}
		tagGroups[p.tagValue] = append(tagGroups[p.tagValue], assetID)
		if albumID != nil {
			albumIDs = append(albumIDs, assetID)
		}
		e := st[p.f.Rel]
		e.Checksum, e.AssetID, e.Size, e.MtimeUnix = checksums[p.f.AbsPath], assetID.String(), p.f.Size, p.f.ModTime.Unix()
		st[p.f.Rel] = e
	}

	// Link duplicates/uploads to tags + optional album.
	for tv, ids := range tagGroups {
		if err := TagAssets(ctx, c, ids, tagIDs[tv]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: tagging %d asset(s) with %q: %v\n", len(ids), tv, err)
			stats.Failed += len(ids)
		}
	}
	if albumID != nil && len(albumIDs) > 0 {
		for i := 0; i < len(albumIDs); i += 500 {
			end := min(i+500, len(albumIDs))
			resp, err := c.API.AddAssetsToAlbumWithResponse(ctx, *albumID, immichapi.BulkIdsDto{Ids: albumIDs[i:end]})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: adding %d asset(s) to album: %v\n", end-i, err)
				stats.Failed += end - i
			} else if resp.JSON200 != nil {
				for _, r := range *resp.JSON200 {
					if !r.Success {
						fmt.Fprintf(os.Stderr, "Error: asset %s: %v\n", r.Id, r.Error)
						stats.Failed++
					}
				}
			}
		}
	}

	if !opts.DryRun {
		if err := saveWatchUploadState(opts.WatchDir, st); err != nil {
			return stats, err
		}
	}
	if stats.Failed > 0 {
		return stats, fmt.Errorf("%d file(s) failed", stats.Failed)
	}
	return stats, nil
}
