package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// assetsUploadCommand exposes POST /assets (uploadAsset).
func assetsUploadCommand() *cli.Command {
	return &cli.Command{
		Name:      "upload",
		Usage:     "Upload one or more files as new assets (POST /assets)",
		ArgsUsage: "[FILE ...]",
		Description: "Uploads each local file as a new asset (multipart/form-data). " +
			"Timestamps default to the file's modification time; a 200 response means the " +
			"server matched the checksum to an existing asset (duplicate) and is reported as an error.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "file-created-at", Usage: "override file creation date (RFC3339, default: file mtime)"},
			&cli.StringFlag{Name: "file-modified-at", Usage: "override file modification date (RFC3339, default: file mtime)"},
			&cli.StringFlag{Name: "filename", Usage: "override the uploaded file name (single FILE only)"},
			&cli.IntFlag{Name: "duration", Usage: "duration in milliseconds (for videos)"},
			&cli.StringFlag{Name: "is-favorite", Usage: "mark as favorite: true or false"},
			&cli.StringFlag{Name: "visibility", Usage: "asset visibility: archive, hidden, locked, or timeline"},
			&cli.StringFlag{Name: "live-photo-video-id", Usage: "live photo video asset `UUID`"},
			&cli.StringFlag{Name: "sidecar", Usage: "sidecar file to upload alongside (single FILE only)"},
			&cli.StringFlag{Name: "key", Usage: "shared-link key (query param)"},
			&cli.StringFlag{Name: "slug", Usage: "shared-link slug (query param)"},
			&cli.StringFlag{Name: "checksum", Usage: "sha1 checksum for duplicate detection before upload (x-immich-checksum header)"},
			&cli.BoolFlag{Name: "json", Usage: "print results as a JSON array"},
			&cli.BoolFlag{Name: "quiet", Usage: "disable per-file progress bars on stderr (--json implies quiet)"},
		},
		Action: assetsUpload,
	}
}

type uploadResult struct {
	File string `json:"file"`
	ID   string `json:"id"`
}

func assetsUpload(ctx context.Context, cmd *cli.Command) error {
	files := cmd.Args().Slice()
	if len(files) == 0 {
		return fmt.Errorf("no files given: pass them as arguments")
	}
	if n := len(files); n > 1 {
		if cmd.String("filename") != "" {
			return fmt.Errorf("--filename can only be used with a single FILE (got %d files)", n)
		}
		if cmd.String("sidecar") != "" {
			return fmt.Errorf("--sidecar can only be used with a single FILE (got %d files)", n)
		}
	}

	opts, err := buildUploadOptions(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	var results []uploadResult
	failures := 0
	quiet := cmd.Bool("quiet") || cmd.Bool("json")
	for i, f := range files {
		fi, serr := os.Stat(f)
		var total int64 = -1
		if serr == nil && !fi.IsDir() {
			total = fi.Size()
		}
		prog := workflows.NewByteProgress(f, total, i+1, len(files), quiet)
		opts.Progress = prog
		id, err := workflows.UploadAssetFile(ctx, c, f, opts)
		prog.Finish()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: file %s: %v\n", f, err)
			failures++
			continue
		}
		results = append(results, uploadResult{File: f, ID: id.String()})
		if !cmd.Bool("json") {
			fmt.Printf("%s -> %s\n", f, id)
		}
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if results == nil {
			results = []uploadResult{}
		}
		if err := enc.Encode(results); err != nil {
			return err
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d files failed", failures, len(files))
	}
	return nil
}

// buildUploadOptions maps CLI flags to workflows.UploadOptions. Every field
// is optional; nil leaves the server default (timestamps fall back to mtime).
func buildUploadOptions(cmd *cli.Command) (workflows.UploadOptions, error) {
	var opts workflows.UploadOptions

	if v := cmd.String("file-created-at"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return opts, fmt.Errorf("invalid --file-created-at %q: %w (expected RFC3339)", v, err)
		}
		opts.FileCreatedAt = &t
	}
	if v := cmd.String("file-modified-at"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return opts, fmt.Errorf("invalid --file-modified-at %q: %w (expected RFC3339)", v, err)
		}
		opts.FileModifiedAt = &t
	}
	if v := cmd.String("filename"); v != "" {
		opts.Filename = &v
	}
	if cmd.IsSet("duration") {
		d := int(cmd.Int("duration"))
		if d < 0 {
			return opts, fmt.Errorf("invalid --duration %d: must not be negative", d)
		}
		opts.Duration = &d
	}
	var fav *bool
	if err := setBoolFlag(cmd, "is-favorite", func(b bool) { fav = &b }); err != nil {
		return opts, err
	}
	opts.IsFavorite = fav
	if v := cmd.String("visibility"); v != "" {
		vis, err := resolveAssetVisibility(v)
		if err != nil {
			return opts, err
		}
		opts.Visibility = &vis
	}
	if v := cmd.String("live-photo-video-id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return opts, fmt.Errorf("invalid --live-photo-video-id %q: %w", v, err)
		}
		uid := openapi_types.UUID(id)
		opts.LivePhotoVideoId = &uid
	}
	if v := cmd.String("sidecar"); v != "" {
		opts.SidecarPath = v
	}
	if v := cmd.String("key"); v != "" {
		opts.Key = &v
	}
	if v := cmd.String("slug"); v != "" {
		opts.Slug = &v
	}
	if v := cmd.String("checksum"); v != "" {
		opts.Checksum = &v
	}
	return opts, nil
}

// resolveAssetVisibility validates --visibility against the spec enum.
func resolveAssetVisibility(raw string) (immichapi.AssetVisibility, error) {
	switch vis := immichapi.AssetVisibility(raw); vis {
	case immichapi.Archive, immichapi.Hidden, immichapi.Locked, immichapi.Timeline:
		return vis, nil
	default:
		return "", fmt.Errorf("invalid --visibility %q: must be one of archive, hidden, locked, timeline", raw)
	}
}
