package workflows

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// RepairModeAll runs every registered safe strategy in order until one
	// applies. Currently equivalent to "marker" but future-proof: adding a new
	// strategy automatically extends "all".
	RepairModeAll RepairMode = "all"
)

// ParseRepairMode validates s and returns the corresponding RepairMode.
func ParseRepairMode(s string) (RepairMode, error) {
	switch RepairMode(s) {
	case RepairModeMarker:
		return RepairModeMarker, nil
	case RepairModeAll:
		return RepairModeAll, nil
	default:
		return "", fmt.Errorf("invalid --mode %q: valid modes are %q, %q", s, RepairModeMarker, RepairModeAll)
	}
}

// JPEGAnalysis is the cheap byte-level classification of a JPEG file used to
// decide whether (and which) repair strategy applies. It deliberately does NOT
// attempt a full image/jpeg decode: Go's decoder is far stricter than Immich's
// libjpeg/libvips and rejects files Immich accepts, so a decode is neither a
// reliable corruption detector nor a reliable repair verifier here. The
// authoritative "is it repaired" check is server-side (see waitForThumbhash).
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

// strategiesForMode returns the strategies to try for the given mode.
func strategiesForMode(mode RepairMode) []RepairStrategy {
	switch mode {
	case RepairModeMarker:
		return []RepairStrategy{markerStrategy{}}
	default: // RepairModeAll
		return repairStrategies
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
	// OutcomeSkippedNonJPEG means the asset is not a JPEG image and was skipped.
	OutcomeSkippedNonJPEG RepairOutcome = "skipped-non-jpeg"
	// OutcomeUnrepairable means the file is damaged beyond what any strategy in
	// this mode can fix (e.g. missing SOI marker).
	OutcomeUnrepairable RepairOutcome = "unrepairable"
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
	// VerifyTimeout bounds how long to wait for Immich to generate a thumbhash
	// on the re-imported asset before treating the repair as failed. Zero means
	// the ReplaceAsset default.
	VerifyTimeout time.Duration
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

// RepairAsset attempts to repair one asset and, on success, re-imports it via
// the replace-asset flow (upload → checksum verify → copy metadata → thumbhash
// verify → remove original). It returns a RepairOutcome describing what
// happened. A non-nil error means the asset failed (and, unless KeepOriginal,
// the original was left untouched — removal only ever runs last, after Immich
// has confirmed the repaired file by generating a thumbhash).
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

	// The marker strategies only apply to JPEG images.
	if info.Type != immichapi.IMAGE || !isJPEGName(info.OriginalFileName) {
		fmt.Printf("%s: skipped (%s, not a JPEG image)\n", assetID, info.Type)
		return OutcomeSkippedNonJPEG, nil
	}

	// Download the original into the per-run temp dir so we can inspect and
	// repair the actual bytes Immich holds.
	srcPath := filepath.Join(opts.TempDir, assetID.String()+"_orig"+filepath.Ext(info.OriginalFileName))
	if err := downloadOriginalTo(ctx, c, assetID, srcPath); err != nil {
		return "", fmt.Errorf("downloading original: %w", err)
	}
	defer os.Remove(srcPath)

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

	repairedPath := filepath.Join(opts.TempDir, assetID.String()+"_repaired"+filepath.Ext(info.OriginalFileName))
	if err := chosen.Repair(srcPath, repairedPath); err != nil {
		return "", fmt.Errorf("%s repair failed: %w", chosen.Name(), err)
	}
	defer os.Remove(repairedPath)

	fmt.Printf("%s: applying %q repair, re-importing repaired file\n", assetID, chosen.Name())

	if err := ReplaceAsset(ctx, c, ReplacePair{AssetID: assetID, NewFilePath: repairedPath}, ReplaceAssetOptions{
		DryRun:            opts.DryRun,
		Force:             opts.Force,
		KeepOriginal:      opts.KeepOriginal,
		VerifyProcessed:   true,
		VerifyTimeout:     opts.VerifyTimeout,
		RollbackOnFailure: true,
	}); err != nil {
		return "", err
	}

	return OutcomeRepaired, nil
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
