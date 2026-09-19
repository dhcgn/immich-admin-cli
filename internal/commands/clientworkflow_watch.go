package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// tagValueByID looks up a tag's full-path value by ID (for display).
func tagValueByID(ctx context.Context, c *client.Client, id openapi_types.UUID) (string, error) {
	resp, err := c.API.GetTagByIdWithResponse(ctx, id)
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return "", err
	}
	if resp.JSON200 == nil {
		return "", fmt.Errorf("tag %s: response had no body", id)
	}
	return resp.JSON200.Value, nil
}

func watchUploadCommand() *cli.Command {
	return &cli.Command{
		Name:  "watch-upload",
		Usage: "Watch a folder and auto-upload new stable files (POST /assets/bulk-upload-check + POST /assets)",
		Description: "Polls --watch-dir every --interval (default 60s) and uploads new files. " +
			"Files whose size/mtime changed within --stable-for (default 30s) are deferred to the next interval. " +
			"Duplicates (bulk-upload-check reject/duplicate with assetId) are only linked to the tag/album, never re-uploaded. " +
			"--mode flat tags everything with --tag-pattern (default immich-admin-cli/watch/{yyyy-MM-dd}); " +
			"--mode by-subfolder uses the raw subfolder name. --album-id/--album-name is opt-in. --once runs a single scan (for cron).",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "watch-dir", Usage: "local directory to watch", Required: true},
			&cli.StringFlag{Name: "mode", Usage: "flat or by-subfolder", Value: "flat"},
			&cli.StringFlag{Name: "tag-pattern", Usage: "tag pattern for --mode flat ({yyyy-MM-dd} = upload day)", Value: workflows.DefaultWatchUploadTagPattern},
			&cli.IntFlag{Name: "depth", Usage: "subfolder depth (v1: only 1 supported)", Value: 1},
			&cli.StringFlag{Name: "album-id", Usage: "opt-in album `ID` to also add uploads to"},
			&cli.StringFlag{Name: "album-name", Usage: "opt-in album name to also add uploads to (created unless --dry-run)"},
			&cli.StringFlag{Name: "interval", Usage: "poll interval (e.g. 60s); <=0 means run once", Value: "60s"},
			&cli.StringFlag{Name: "stable-for", Usage: "defer files changed within this long (e.g. 30s)", Value: "30s"},
			&cli.BoolFlag{Name: "once", Usage: "run a single scan and exit (for cron)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "print what would be uploaded/linked without changing anything"},
			&cli.BoolFlag{Name: "yes", Usage: "skip creation prompts for tags/albums"},
			&cli.BoolFlag{Name: "quiet", Usage: "disable per-file progress bars on stderr (--json implies quiet)"},
			&cli.BoolFlag{Name: "json", Usage: "print per-interval stats as JSON on stdout"},
		},
		Action: clientWorkflowWatchUpload,
	}
}

func watchDownloadCommand() *cli.Command {
	return &cli.Command{
		Name:  "watch-download",
		Usage: "Keep a local folder in sync from an album or tag (loop around download-album --sync)",
		Description: "Polls exactly one source (--album-id|--album-name or --tag-id|--tag-value) every --interval " +
			"(default 300s) and mirrors it into --target-dir using the .immich-sync.json manifest: skip unchanged, " +
			"re-download changed, delete locals whose asset left the source, never touch untracked files. --once runs a single scan.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "album-id", Usage: "album `ID` source (mutually exclusive with --album-name/--tag-*)"},
			&cli.StringFlag{Name: "album-name", Usage: "album name source"},
			&cli.StringFlag{Name: "tag-id", Usage: "tag `ID` source (mutually exclusive with --tag-value/album-*)"},
			&cli.StringFlag{Name: "tag-value", Usage: "tag value (full path) source"},
			&cli.StringFlag{Name: "target-dir", Usage: "local directory to sync into (created if missing)", Required: true},
			&cli.StringFlag{Name: "size", Usage: "media variant: original, fullsize, preview, or thumbnail", Value: string(immichapi.AssetMediaSizeOriginal)},
			&cli.StringFlag{Name: "interval", Usage: "poll interval (e.g. 300s); <=0 means run once", Value: "300s"},
			&cli.BoolFlag{Name: "once", Usage: "run a single sync and exit (for cron)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "print the planned sync without changing anything"},
			&cli.BoolFlag{Name: "yes", Usage: "skip the deletion confirmation prompt"},
			&cli.BoolFlag{Name: "quiet", Usage: "disable per-file progress bars on stderr (--json implies quiet)"},
			&cli.BoolFlag{Name: "json", Usage: "print per-interval stats as JSON on stdout"},
		},
		Action: clientWorkflowWatchDownload,
	}
}

func parseIntervalOrOnce(raw string, once bool) (time.Duration, bool, error) {
	if raw == "" {
		return 0, true, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false, fmt.Errorf("invalid --interval %q: %w (e.g. 60s, 5m)", raw, err)
	}
	if d <= 0 {
		return 0, true, nil
	}
	return d, once, nil
}

func clientWorkflowWatchUpload(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() > 0 {
		return fmt.Errorf("watch-upload takes no positional arguments (got %v)", cmd.Args().Slice())
	}
	mode := cmd.String("mode")
	if mode != "flat" && mode != "by-subfolder" {
		return fmt.Errorf("invalid --mode %q: must be flat or by-subfolder", mode)
	}
	stableFor, err := time.ParseDuration(cmd.String("stable-for"))
	if err != nil {
		return fmt.Errorf("invalid --stable-for %q: %w", cmd.String("stable-for"), err)
	}
	if stableFor < 0 {
		return fmt.Errorf("invalid --stable-for %q: must not be negative", cmd.String("stable-for"))
	}
	interval, once, err := parseIntervalOrOnce(cmd.String("interval"), cmd.Bool("once"))
	if err != nil {
		return err
	}
	var albumID *openapi_types.UUID
	if s := cmd.String("album-id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			return fmt.Errorf("invalid --album-id %q: %w", s, err)
		}
		uid := openapi_types.UUID(id)
		albumID = &uid
	}
	if cmd.String("album-id") != "" && cmd.String("album-name") != "" {
		return fmt.Errorf("only one of --album-id or --album-name may be given")
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	opts := workflows.WatchUploadOptions{
		WatchDir:   cmd.String("watch-dir"),
		Mode:       mode,
		Depth:      int(cmd.Int("depth")),
		TagPattern: cmd.String("tag-pattern"),
		AlbumID:    albumID,
		AlbumName:  cmd.String("album-name"),
		Interval:   interval,
		StableFor:  stableFor,
		Once:       once,
		DryRun:     cmd.Bool("dry-run"),
		Yes:        cmd.Bool("yes"),
		Quiet:      cmd.Bool("quiet"),
		JSON:       cmd.Bool("json"),
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	for {
		stats, runErr := workflows.RunWatchUploadOnce(ctx, c, opts)
		printWatchUploadSummary(stats, opts.JSON)
		if runErr != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", runErr)
		}
		if once {
			return runErr
		}
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "Interrupted.")
			return nil
		case <-time.After(interval):
		}
	}
}

func printWatchUploadSummary(stats workflows.WatchUploadStats, asJSON bool) {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		_ = enc.Encode(stats)
		return
	}
	fmt.Fprintf(os.Stderr, "uploaded=%d linked-duplicate=%d skipped-unstable=%d failed=%d\n",
		stats.Uploaded, stats.LinkedDuplicate, stats.SkippedUnstable, stats.Failed)
}

func clientWorkflowWatchDownload(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() > 0 {
		return fmt.Errorf("watch-download takes no positional arguments (got %v)", cmd.Args().Slice())
	}
	set := 0
	for _, v := range []string{cmd.String("album-id"), cmd.String("album-name"), cmd.String("tag-id"), cmd.String("tag-value")} {
		if v != "" {
			set++
		}
	}
	if set != 1 {
		return fmt.Errorf("exactly one source is required: --album-id | --album-name | --tag-id | --tag-value")
	}
	size, err := resolveDownloadAlbumSize(cmd.String("size"))
	if err != nil {
		return err
	}
	interval, once, err := parseIntervalOrOnce(cmd.String("interval"), cmd.Bool("once"))
	if err != nil {
		return err
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	wopts := workflows.WatchDownloadOptions{
		TargetDir: cmd.String("target-dir"),
		Interval:  int64(interval.Seconds()),
		Once:      once,
		DryRun:    cmd.Bool("dry-run"),
		Quiet:     cmd.Bool("quiet"),
		JSON:      cmd.Bool("json"),

		Size: size}
	wopts.Quiet = cmd.Bool("quiet") || cmd.Bool("json")

	// Resolve source once so the loop prints real names and fails fast.
	switch {
	case cmd.String("album-id") != "":
		id, err := uuid.Parse(cmd.String("album-id"))
		if err != nil {
			return fmt.Errorf("invalid --album-id %q: %w", cmd.String("album-id"), err)
		}
		uid := openapi_types.UUID(id)
		album, err := workflows.ResolveAlbum(ctx, c, &uid, "")
		if err != nil {
			return err
		}
		wopts.Source = workflows.SyncSource{Kind: workflows.SourceKindAlbum, ID: album.Id.String(), Name: album.AlbumName}
		wopts.AlbumID = &album.Id
	case cmd.String("album-name") != "":
		album, err := resolveDownloadAlbum(ctx, c, nil, cmd.String("album-name"), cmd.Bool("yes"))
		if err != nil {
			return err
		}
		wopts.Source = workflows.SyncSource{Kind: workflows.SourceKindAlbum, ID: album.Id.String(), Name: album.AlbumName}
		uid := album.Id
		wopts.AlbumID = &uid
	case cmd.String("tag-id") != "":
		id, err := uuid.Parse(cmd.String("tag-id"))
		if err != nil {
			return fmt.Errorf("invalid --tag-id %q: %w", cmd.String("tag-id"), err)
		}
		uid := openapi_types.UUID(id)
		wopts.Source = workflows.SyncSource{Kind: workflows.SourceKindTag, ID: uid.String()}
		wopts.TagID = &uid
		if name, nerr := tagValueByID(ctx, c, uid); nerr == nil {
			wopts.Source.Name = name
		}
	case cmd.String("tag-value") != "":
		tag, err := workflows.ResolveTagByValue(ctx, c, cmd.String("tag-value"))
		if err != nil {
			return err
		}
		wopts.Source = workflows.SyncSource{Kind: workflows.SourceKindTag, ID: tag.Id.String(), Name: tag.Value}
		tid := tag.Id
		wopts.TagID = &tid
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	for {
		plan, runErr := workflows.RunWatchDownloadOnce(ctx, c, wopts)
		printWatchDownloadSummary(wopts.Source, plan, wopts.JSON)
		if runErr != nil && ctx.Err() == nil {
			// ApplySync already prints per-file errors; surface summary.
			fmt.Fprintf(os.Stderr, "Error: %v\n", runErr)
		}
		if wopts.DryRun && (len(plan.Additions) == 0 && len(plan.Updates) == 0 && len(plan.Removals) == 0) {
			fmt.Println("Already up to date.")
		}
		if once {
			return runErr
		}
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "Interrupted.")
			return nil
		case <-time.After(interval):
		}
	}
}

func printWatchDownloadSummary(source workflows.SyncSource, plan workflows.SyncPlan, asJSON bool) {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		_ = enc.Encode(map[string]any{
			"sourceKind": source.Kind, "source": source.Name,
			"add": len(plan.Additions), "update": len(plan.Updates),
			"unchanged": len(plan.Unchanged), "remove": len(plan.Removals),
		})
		return
	}
	fmt.Fprintf(os.Stderr, "%s %q: add=%d update=%d unchanged=%d remove=%d\n",
		source.Kind, source.Name, len(plan.Additions), len(plan.Updates), len(plan.Unchanged), len(plan.Removals))
}
