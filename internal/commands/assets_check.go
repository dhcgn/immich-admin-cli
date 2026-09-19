package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/workflows"
)

// assetsCheckRemoteExistsCommand exposes POST /assets/bulk-upload-check
// (checkBulkUpload) as `assets check-remote-exists` (alias check-bulk-upload).
func assetsCheckRemoteExistsCommand() *cli.Command {
	return &cli.Command{
		Name:    "check-remote-exists",
		Aliases: []string{"check-bulk-upload"},
		Usage:   "Check if local files already exist on the server via checksum (POST /assets/bulk-upload-check)",
		Description: "Hashes each local file (base64 SHA1, the format Immich reports " +
			"as Checksum) and asks the server which checksums it already has. " +
			"Read-only: never uploads, downloads, or mutates anything.",
		ArgsUsage: "[FILE|DIR ...]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "json", Usage: "print results as a JSON array"},
			&cli.BoolFlag{Name: "duplicates-only", Usage: "show only files already on the server"},
			&cli.BoolFlag{Name: "missing-only", Usage: "show only files missing on the server"},
			&cli.BoolFlag{Name: "ids-only", Aliases: []string{"q"}, Usage: "print only duplicate asset IDs, one per line (pipeable into albums add-assets / tags tag)"},
		},
		Action: assetsCheckRemoteExists,
	}
}

type checkRemoteResult struct {
	File     string `json:"file"`
	Checksum string `json:"checksum"`
	Status   string `json:"status"`
	AssetID  string `json:"assetId,omitempty"`
	Trashed  bool   `json:"isTrashed,omitempty"`
}

func assetsCheckRemoteExists(ctx context.Context, cmd *cli.Command) error {
	paths := cmd.Args().Slice()
	if len(paths) == 0 {
		return fmt.Errorf("no files given: pass [FILE|DIR ...]")
	}
	files, err := workflows.CollectCheckFiles(paths)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no files found in %v (hidden files/symlinks are skipped)", paths)
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	// Hash locally first so one unreadable file doesn't abort the server check.
	checksums := make(map[string]string, len(files))
	var hashable []string
	failures := 0
	for i, f := range files {
		fmt.Fprintf(os.Stderr, "[%d/%d] hashing %s\n", i+1, len(files), f)
		sum, herr := workflows.FileSHA1Base64(f)
		if herr != nil {
			fmt.Fprintf(os.Stderr, "Error: file %s: %v\n", f, herr)
			failures++
			continue
		}
		checksums[f] = sum
		hashable = append(hashable, f)
	}

	entries, err := workflows.CheckBulkUploadChecksums(ctx, c, hashable, checksums)
	if err != nil {
		return err
	}

	dupsOnly := cmd.Bool("duplicates-only")
	missOnly := cmd.Bool("missing-only")
	idsOnly := cmd.Bool("ids-only")

	var results []checkRemoteResult
	for _, e := range entries {
		switch {
		case idsOnly && e.Status != workflows.BulkCheckUploaded:
			continue
		case dupsOnly && e.Status != workflows.BulkCheckUploaded:
			continue
		case missOnly && e.Status != workflows.BulkCheckMissing:
			continue
		}
		r := checkRemoteResult{File: e.File, Checksum: e.Checksum, Status: string(e.Status)}
		if e.AssetID != nil {
			r.AssetID = e.AssetID.String()
		}
		r.Trashed = e.IsTrashed
		results = append(results, r)
	}

	if idsOnly {
		for _, r := range results {
			fmt.Println(r.AssetID)
		}
	} else if cmd.Bool("json") {
		if results == nil {
			results = []checkRemoteResult{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			switch r.Status {
			case string(workflows.BulkCheckUploaded):
				fmt.Printf("%s: uploaded %s\n", r.File, r.AssetID)
			case string(workflows.BulkCheckMissing):
				fmt.Printf("%s: missing\n", r.File)
			default:
				fmt.Printf("%s: unsupported\n", r.File)
			}
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d files failed", failures, len(files))
	}
	return nil
}
