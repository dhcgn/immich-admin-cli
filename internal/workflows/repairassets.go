package workflows

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// RepairMode selects which repair strategies the repair-assets workflow runs.
type RepairMode string

const (
	// RepairModeMarker runs only the JPEG End-of-Image marker strategy
	// (append the missing FF D9). Safe and lossless.
	RepairModeMarker RepairMode = "marker"
	// RepairModeTIFFTags runs only the TIFF zero-count IFD tag strategy (patch
	// any IFD entry whose count field is literally 0 to 1 in place). Safe and
	// lossless: layout, pixel data and all other metadata are untouched.
	RepairModeTIFFTags RepairMode = "tiff-tags"
	// RepairModeTakeoutJSON is a DELETE mode, not a repair mode: it identifies
	// assets whose stored bytes are actually a Google Photos Takeout metadata
	// JSON sidecar imported in place of the real photo (a known Takeout
	// export/import failure — the file has no image data and is unrecoverable)
	// and removes them (to trash by default; --force to delete permanently).
	// It is deliberately opt-in only and is NOT included in RepairModeAll,
	// because unlike the repair strategies it deletes an asset outright rather
	// than re-importing a fixed copy.
	RepairModeTakeoutJSON RepairMode = "takeout-json"
	// RepairModeAll runs every registered safe strategy, across all supported
	// file types, in order until one applies. Adding a new strategy to the
	// registries automatically extends "all" — no other code changes needed.
	// It intentionally excludes RepairModeTakeoutJSON (a destructive delete
	// mode), which must always be requested explicitly.
	RepairModeAll RepairMode = "all"
)

// ParseRepairMode validates s and returns the corresponding RepairMode.
func ParseRepairMode(s string) (RepairMode, error) {
	switch RepairMode(s) {
	case RepairModeMarker:
		return RepairModeMarker, nil
	case RepairModeTIFFTags:
		return RepairModeTIFFTags, nil
	case RepairModeTakeoutJSON:
		return RepairModeTakeoutJSON, nil
	case RepairModeAll:
		return RepairModeAll, nil
	default:
		return "", fmt.Errorf("invalid --mode %q: valid modes are %q, %q, %q, %q", s, RepairModeMarker, RepairModeTIFFTags, RepairModeTakeoutJSON, RepairModeAll)
	}
}

// JPEGAnalysis is the cheap byte-level classification of a JPEG file used to
// decide whether (and which) repair strategy applies. It deliberately does NOT
// attempt a full image/jpeg decode: Go's decoder is far stricter than Immich's
// libjpeg/libvips and rejects files Immich accepts, so a decode is neither a
// reliable corruption detector nor a reliable repair verifier here.
type JPEGAnalysis struct {
	// HasSOI reports whether the file starts with the Start-of-Image marker
	// FF D8.
	HasSOI bool
	// HasEOI reports whether the file ends with the End-of-Image marker FF D9.
	HasEOI bool
	// Size is the file size in bytes.
	Size int64
}

// RepairStrategy is one named repair technique. Strategies are registered in
// repairStrategies; adding a new repair mode is done by implementing this
// interface and appending to that slice — the command and orchestration layers
// are untouched.
type RepairStrategy interface {
	// Name is the strategy's short identifier (e.g. "marker").
	Name() string
	// Applicable reports whether this strategy can repair a file with the
	// given analysis.
	Applicable(a JPEGAnalysis) bool
	// Repair reads src and writes a repaired copy to dst. It must not modify
	// src. It is only called when Applicable returned true.
	Repair(src, dst string) error
}

// repairStrategies is the ordered registry of all safe repair strategies.
// Order matters for RepairModeAll (first applicable strategy wins).
var repairStrategies = []RepairStrategy{
	markerStrategy{},
}

// strategiesForMode returns the JPEG strategies to try for the given mode.
func strategiesForMode(mode RepairMode) []RepairStrategy {
	switch mode {
	case RepairModeMarker:
		return []RepairStrategy{markerStrategy{}}
	case RepairModeTIFFTags:
		return nil // TIFF-only mode: no JPEG strategy applies
	default: // RepairModeAll
		return repairStrategies
	}
}

// TIFFRepairStrategy is one named TIFF repair technique, mirroring
// RepairStrategy but keyed on TIFFAnalysis. Kept as a separate interface
// (rather than a generic one) because JPEG and TIFF detection are
// structurally unrelated — this keeps each Applicable() check precise and
// avoids a shared "one size fits all" analysis type.
type TIFFRepairStrategy interface {
	// Name is the strategy's short identifier (e.g. "tiff-zero-count").
	Name() string
	// Applicable reports whether this strategy can repair a file with the
	// given analysis.
	Applicable(a TIFFAnalysis) bool
	// Repair reads src and writes a repaired copy to dst. It must not modify
	// src. It is only called when Applicable returned true.
	Repair(src, dst string) error
}

// tiffRepairStrategies is the ordered registry of all safe TIFF repair
// strategies.
var tiffRepairStrategies = []TIFFRepairStrategy{
	tiffZeroCountStrategy{},
}

// tiffStrategiesForMode returns the TIFF strategies to try for the given mode.
func tiffStrategiesForMode(mode RepairMode) []TIFFRepairStrategy {
	switch mode {
	case RepairModeMarker:
		return nil // JPEG-only mode: no TIFF strategy applies
	default: // RepairModeTIFFTags or RepairModeAll
		return tiffRepairStrategies
	}
}

// RepairOutcome classifies what happened to one asset, for the batch summary.
type RepairOutcome string

const (
	// OutcomeRepaired means the file was repaired and re-imported.
	OutcomeRepaired RepairOutcome = "repaired"
	// OutcomeAlreadyOK means no strategy applied because the file is not
	// missing anything this mode repairs (e.g. it already has an EOI marker).
	OutcomeAlreadyOK RepairOutcome = "already-ok"
	// OutcomeSkippedUnsupported means the asset has no applicable repair
	// strategy for its type/extension (e.g. not a JPEG or TIFF image) and was
	// skipped without attempting anything.
	OutcomeSkippedUnsupported RepairOutcome = "skipped-unsupported"
	// OutcomeUnrepairable means the file is damaged beyond what any strategy in
	// this mode can fix (e.g. missing SOI marker, or a TIFF whose IFD chain
	// could not be walked / has no recognized zero-count defect).
	OutcomeUnrepairable RepairOutcome = "unrepairable"
	// OutcomeDeletedSidecar means the asset was confirmed (structurally) to be
	// a Google Photos Takeout JSON sidecar imported in place of the real photo
	// and was deleted (takeout-json mode).
	OutcomeDeletedSidecar RepairOutcome = "deleted-sidecar"
)

// RepairAssetsOptions controls the repair-assets workflow.
type RepairAssetsOptions struct {
	// Mode selects the repair strategies to try.
	Mode RepairMode
	// DryRun prints the planned steps without changing anything.
	DryRun bool
	// Force permanently deletes the original instead of trashing it.
	Force bool
	// KeepOriginal repairs and re-imports but leaves the original untouched.
	KeepOriginal bool
	// TempDir is the per-run scratch directory downloaded originals and
	// repaired copies are written to. It must exist for the duration of the run.
	TempDir string
}

// jpegExtensions are the file extensions treated as JPEG for the JPEG-only
// repair strategies.
var jpegExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".jpe":  true,
	".jfif": true,
}

// tiffExtensions are the file extensions treated as TIFF for the TIFF-only
// repair strategies.
var tiffExtensions = map[string]bool{
	".tif":  true,
	".tiff": true,
}

// RepairAsset attempts to repair one asset and, on success, re-imports it via
// the replace-asset flow (upload → checksum verify → copy metadata → remove
// original). It returns a RepairOutcome describing what happened. A non-nil
// error means the asset failed (and, unless KeepOriginal, the original was
// left untouched — removal only ever runs last, after the upload and metadata
// copy succeeded). Note that removal is NOT gated on Immich having generated
// a thumbhash for the new asset yet: server-side thumbnail generation is
// asynchronous and its timing is affected by too many factors (queue depth,
// job scheduling, server load) to reliably bound with a timeout, so
// repair-assets no longer waits for it. Use `find-no-thumbhash` afterwards to
// confirm a repair actually produced a thumbnail, or pass --keep-original to
// be able to re-check before the original is gone.
func RepairAsset(ctx context.Context, c *client.Client, assetID openapi_types.UUID, opts RepairAssetsOptions) (RepairOutcome, error) {
	// Fetch asset info to learn the type and original file name.
	infoResp, err := c.API.GetAssetInfoWithResponse(ctx, assetID, nil)
	if err == nil {
		err = client.Check(infoResp, http.StatusOK)
	}
	if err != nil {
		return "", fmt.Errorf("fetching asset info: %w", err)
	}
	info := infoResp.JSON200

	// takeout-json is a delete mode, not a repair mode: it applies to a file of
	// any extension (the corrupt sidecar keeps the real photo's name) and does
	// not go through the repair→replace path, so handle it before the
	// JPEG/TIFF gate below.
	if opts.Mode == RepairModeTakeoutJSON {
		return repairTakeoutSidecar(ctx, c, assetID, info, opts)
	}

	ext := strings.ToLower(filepath.Ext(info.OriginalFileName))
	isJPEG := info.Type == immichapi.IMAGE && jpegExtensions[ext]
	isTIFF := info.Type == immichapi.IMAGE && tiffExtensions[ext]
	if !isJPEG && !isTIFF {
		// The repair strategies can't touch this file type, but it might still
		// be a Google Takeout JSON sidecar (which can have any extension, e.g.
		// a .dng). Probe just the head remotely — without downloading the whole
		// file — so we can point the user at takeout-json mode when it applies.
		if sc, _ := probeTakeoutSidecarHead(ctx, c, assetID); sc.IsSidecar {
			fmt.Printf("%s: is a Google Takeout JSON sidecar (original title %q), not a repairable image — rerun with --mode %s to delete it\n", assetID, sc.Title, RepairModeTakeoutJSON)
			return OutcomeUnrepairable, nil
		}
		fmt.Printf("%s: skipped (%s, no applicable repair strategy for this file type)\n", assetID, info.Type)
		return OutcomeSkippedUnsupported, nil
	}

	// Download the original into the per-run temp dir so we can inspect and
	// repair the actual bytes Immich holds.
	srcPath := filepath.Join(opts.TempDir, assetID.String()+"_orig"+filepath.Ext(info.OriginalFileName))
	if err := downloadOriginalTo(ctx, c, assetID, srcPath); err != nil {
		return "", fmt.Errorf("downloading original: %w", err)
	}
	defer os.Remove(srcPath)

	// A file whose bytes are actually a Google Takeout JSON sidecar is not a
	// repairable image (it has no image data at all). The repair modes can't
	// fix it, but the dedicated takeout-json mode can remove it — surface that
	// hint here rather than reporting an opaque "unrepairable". The probe reuses
	// the already-downloaded bytes, so it costs nothing extra.
	if sc, _ := analyzeTakeoutSidecar(srcPath); sc.IsSidecar {
		fmt.Printf("%s: is a Google Takeout JSON sidecar (original title %q), not a repairable image — rerun with --mode %s to delete it\n", assetID, sc.Title, RepairModeTakeoutJSON)
		return OutcomeUnrepairable, nil
	}

	repairedPath := filepath.Join(opts.TempDir, assetID.String()+"_repaired"+filepath.Ext(info.OriginalFileName))
	var strategyName string

	switch {
	case isJPEG:
		analysis, err := analyzeJPEG(srcPath)
		if err != nil {
			return "", fmt.Errorf("analysing JPEG: %w", err)
		}

		// Pick the first applicable strategy for the mode.
		var chosen RepairStrategy
		for _, s := range strategiesForMode(opts.Mode) {
			if s.Applicable(analysis) {
				chosen = s
				break
			}
		}
		if chosen == nil {
			if analysis.HasEOI {
				fmt.Printf("%s: already OK (has SOI and EOI markers); no %s repair applies\n", assetID, opts.Mode)
				return OutcomeAlreadyOK, nil
			}
			return OutcomeUnrepairable, fmt.Errorf("no applicable repair strategy for mode %q (hasSOI=%t hasEOI=%t)", opts.Mode, analysis.HasSOI, analysis.HasEOI)
		}
		if err := chosen.Repair(srcPath, repairedPath); err != nil {
			return "", fmt.Errorf("%s repair failed: %w", chosen.Name(), err)
		}
		strategyName = chosen.Name()

	case isTIFF:
		analysis, err := analyzeTIFF(srcPath)
		if err != nil {
			return "", fmt.Errorf("analysing TIFF: %w", err)
		}

		// Pick the first applicable strategy for the mode.
		var chosen TIFFRepairStrategy
		for _, s := range tiffStrategiesForMode(opts.Mode) {
			if s.Applicable(analysis) {
				chosen = s
				break
			}
		}
		if chosen == nil {
			if analysis.Valid {
				fmt.Printf("%s: already OK (no recognized malformed IFD tags); no %s repair applies\n", assetID, opts.Mode)
				return OutcomeAlreadyOK, nil
			}
			return OutcomeUnrepairable, fmt.Errorf("no applicable repair strategy for mode %q (TIFF header/IFD chain could not be parsed)", opts.Mode)
		}
		if err := chosen.Repair(srcPath, repairedPath); err != nil {
			return "", fmt.Errorf("%s repair failed: %w", chosen.Name(), err)
		}
		strategyName = chosen.Name()
	}
	defer os.Remove(repairedPath)

	fmt.Printf("%s: applying %q repair, re-importing repaired file\n", assetID, strategyName)

	if err := ReplaceAsset(ctx, c, ReplacePair{AssetID: assetID, NewFilePath: repairedPath}, ReplaceAssetOptions{
		DryRun:            opts.DryRun,
		Force:             opts.Force,
		KeepOriginal:      opts.KeepOriginal,
		RollbackOnFailure: true,
	}); err != nil {
		return "", err
	}

	return OutcomeRepaired, nil
}

// repairTakeoutSidecar implements the takeout-json mode. It downloads the
// asset, structurally confirms the bytes are a Google Photos Takeout metadata
// JSON sidecar that was imported in place of the real photo, and — because
// such a file has no recoverable image data — deletes it (to trash by default;
// opts.Force deletes permanently). Files that are not a confirmed sidecar are
// left completely untouched and reported as skipped. In dry-run it reports what
// it would delete without calling the delete API.
func repairTakeoutSidecar(ctx context.Context, c *client.Client, assetID openapi_types.UUID, info *immichapi.AssetResponseDto, opts RepairAssetsOptions) (RepairOutcome, error) {
	srcPath := filepath.Join(opts.TempDir, assetID.String()+"_probe"+filepath.Ext(info.OriginalFileName))
	if err := downloadOriginalTo(ctx, c, assetID, srcPath); err != nil {
		return "", fmt.Errorf("downloading original: %w", err)
	}
	defer os.Remove(srcPath)

	analysis, err := analyzeTakeoutSidecar(srcPath)
	if err != nil {
		return "", fmt.Errorf("analysing takeout sidecar: %w", err)
	}
	if !analysis.IsSidecar {
		fmt.Printf("%s: skipped (%s %q is not a Google Takeout JSON sidecar)\n", assetID, info.Type, info.OriginalFileName)
		return OutcomeSkippedUnsupported, nil
	}

	disposition := "trash"
	if opts.Force {
		disposition = "permanent delete"
	}
	fmt.Printf("%s: confirmed Google Takeout JSON sidecar (original title %q, %d-byte JSON header); deleting (%s)\n", assetID, analysis.Title, analysis.JSONSize, disposition)

	if opts.DryRun {
		fmt.Printf("[dry-run] %s: would delete corrupt sidecar asset (%s)\n", assetID, disposition)
		return OutcomeDeletedSidecar, nil
	}

	if err := removeOriginalAsset(ctx, c, assetID, opts.Force); err != nil {
		return "", err
	}
	return OutcomeDeletedSidecar, nil
}

// isJPEGName reports whether name has a JPEG file extension.
func isJPEGName(name string) bool {
	return jpegExtensions[strings.ToLower(filepath.Ext(name))]
}

// analyzeJPEG reads the first and last two bytes of the file at path to detect
// the SOI (FF D8) and EOI (FF D9) markers.
func analyzeJPEG(path string) (JPEGAnalysis, error) {
	f, err := os.Open(path)
	if err != nil {
		return JPEGAnalysis{}, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return JPEGAnalysis{}, err
	}
	a := JPEGAnalysis{Size: fi.Size()}

	if a.Size >= 2 {
		soi := make([]byte, 2)
		if _, err := f.ReadAt(soi, 0); err == nil {
			a.HasSOI = soi[0] == 0xFF && soi[1] == 0xD8
		}
		eoi := make([]byte, 2)
		if _, err := f.ReadAt(eoi, a.Size-2); err == nil {
			a.HasEOI = eoi[0] == 0xFF && eoi[1] == 0xD9
		}
	}
	return a, nil
}

// probeTakeoutSidecarHead streams only the first maxSidecarProbeBytes of an
// asset's original bytes and reports whether they are a Google Takeout JSON
// sidecar. It exists so repair modes can flag an unsupported-extension file
// (e.g. a .dng that is actually a sidecar) without downloading the whole file:
// it reads the head and closes the connection. Any transport error is returned
// so callers can treat "couldn't probe" as "not a sidecar" without masking it.
func probeTakeoutSidecarHead(ctx context.Context, c *client.Client, id openapi_types.UUID) (TakeoutSidecarAnalysis, error) {
	resp, err := c.API.DownloadAsset(ctx, id, nil)
	if err != nil {
		return TakeoutSidecarAnalysis{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return TakeoutSidecarAnalysis{}, fmt.Errorf("server returned %s (expected 200): %s", resp.Status, strings.TrimSpace(string(body)))
	}

	head, err := io.ReadAll(io.LimitReader(resp.Body, maxSidecarProbeBytes))
	if err != nil {
		return TakeoutSidecarAnalysis{}, err
	}
	return analyzeTakeoutSidecarBytes(head), nil
}

// downloadOriginalTo streams GET /assets/{id}/original to destPath. It uses the
// raw generated method (not the buffering ...WithResponse variant) so multi-GB
// assets are never loaded fully into memory.
func downloadOriginalTo(ctx context.Context, c *client.Client, id openapi_types.UUID, destPath string) error {
	resp, err := c.API.DownloadAsset(ctx, id, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("server returned %s (expected 200): %s", resp.Status, strings.TrimSpace(string(body)))
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating temp file %q: %w", destPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("writing temp file %q: %w", destPath, err)
	}
	return nil
}
