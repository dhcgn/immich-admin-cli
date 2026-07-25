package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
)

// assetsDownloadCommand returns the `download-original` subcommand
// (GET /assets/{id}/original, operationId downloadAsset).
func assetsDownloadCommand() *cli.Command {
	return &cli.Command{
		Name:      "download-original",
		Usage:     "Download original asset files (GET /assets/{id}/original)",
		ArgsUsage: "[ASSET_ID ...]",
		Flags: []cli.Flag{
			idsFileFlag(),
			&cli.StringFlag{
				Name:  "out-dir",
				Usage: "directory to save downloaded files into (default: current working directory)",
			},
		},
		Action: assetsDownloadOriginal,
	}
}

func assetsDownloadOriginal(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	outDir := cmd.String("out-dir")
	if outDir == "" {
		outDir = "."
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory %q: %w", outDir, err)
	}

	// Fan out over the single-asset download endpoint: continue on per-ID
	// errors and report failures at the end (same convention as assetsInfo).
	failures := 0
	for _, id := range ids {
		if err := downloadOneAsset(ctx, c, id, outDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: asset %s: %v\n", id, err)
			failures++
			continue
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d assets failed", failures, len(ids))
	}
	return nil
}

func downloadOneAsset(ctx context.Context, c *client.Client, id openapi_types.UUID, outDir string) error {
	infoResp, err := c.API.GetAssetInfoWithResponse(ctx, id, nil)
	if err == nil {
		err = client.Check(infoResp, http.StatusOK)
	}
	if err != nil {
		return fmt.Errorf("fetching asset info: %w", err)
	}
	originalFileName := infoResp.JSON200.OriginalFileName

	// DownloadAsset returns application/octet-stream, so the raw generated
	// method is used (never the buffering ...WithResponse variant) to avoid
	// loading multi-GB assets fully into memory.
	resp, err := c.API.DownloadAsset(ctx, id, nil)
	if err != nil {
		return fmt.Errorf("downloading asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("server returned %s (expected 200): %s", resp.Status, strings.TrimSpace(string(body)))
	}

	idStr := id.String()
	destPath := filepath.Join(outDir, idStr+"_"+originalFileName)

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating output file %q: %w", destPath, err)
	}
	defer f.Close()

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("writing output file %q: %w", destPath, err)
	}

	fmt.Printf("Saved %s (%s)\n", destPath, formatBytesInt64(written))
	return nil
}

// formatBytesInt64 renders a byte count human-readably.
func formatBytesInt64(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
