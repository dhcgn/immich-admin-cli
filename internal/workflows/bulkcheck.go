package workflows

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// BulkCheckStatus is the per-file outcome of a bulk-upload-check.
type BulkCheckStatus string

const (
	BulkCheckUploaded    BulkCheckStatus = "uploaded"
	BulkCheckMissing     BulkCheckStatus = "missing"
	BulkCheckUnsupported BulkCheckStatus = "unsupported"
)

// BulkCheckEntry is one file's bulk-upload-check result.
type BulkCheckEntry struct {
	File      string
	Checksum  string
	Status    BulkCheckStatus
	AssetID   *openapi_types.UUID
	IsTrashed bool
}

// CollectCheckFiles expands FILE|DIR args into files. Dirs are walked
// recursively; hidden files/dirs (base starting with '.') and symlinks are
// skipped. Pure filesystem, no network.
func CollectCheckFiles(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		fi, err := os.Lstat(p)
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", p, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !fi.IsDir() {
			if strings.HasPrefix(filepath.Base(p), ".") {
				continue
			}
			out = append(out, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			base := filepath.Base(path)
			if d.Type()&os.ModeSymlink != 0 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(base, ".") {
				if d.IsDir() && path != p {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walking %q: %w", p, err)
		}
	}
	return out, nil
}

// FileSHA1Base64 returns the base64-standard-encoded SHA1 hash of the file
// at path, matching Immich's Checksum format. Exported for the native
// check command; workflows reuse the same helper as replace-asset verify.
func FileSHA1Base64(path string) (string, error) {
	return fileSHA1Base64(path)
}

// CheckBulkUploadChecksums calls POST /assets/bulk-upload-check for files
// (checksums[file] must be base64 SHA1) in ~500/request chunks and strictly
// classifies each result. Pure network wrapper; hashing happens outside so
// callers can continue-on-error per file.
func CheckBulkUploadChecksums(ctx context.Context, c *client.Client, files []string, checksums map[string]string) ([]BulkCheckEntry, error) {
	var out []BulkCheckEntry
	for i := 0; i < len(files); i += 500 {
		end := min(i+500, len(files))
		chunk := files[i:end]
		items := make([]immichapi.AssetBulkUploadCheckItem, 0, len(chunk))
		for _, f := range chunk {
			items = append(items, immichapi.AssetBulkUploadCheckItem{Checksum: checksums[f], Id: f})
		}
		resp, err := c.API.CheckBulkUploadWithResponse(ctx, immichapi.AssetBulkUploadCheckDto{Assets: items})
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			return nil, fmt.Errorf("calling POST /assets/bulk-upload-check: %w", err)
		}
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("calling POST /assets/bulk-upload-check: response had no body")
		}
		for _, r := range resp.JSON200.Results {
			out = append(out, ClassifyBulkCheckResult(r.Id, checksums[r.Id], r))
		}
	}
	return out, nil
}

// ClassifyBulkCheckResult strictly splits duplicate vs unsupported per
// AssetBulkUploadCheckResult. Pure for testing.
func ClassifyBulkCheckResult(file, checksum string, r immichapi.AssetBulkUploadCheckResult) BulkCheckEntry {
	e := BulkCheckEntry{File: file, Checksum: checksum}
	switch {
	case r.Action == immichapi.Accept:
		e.Status = BulkCheckMissing
	case r.Action == immichapi.Reject && r.Reason != nil &&
		*r.Reason == immichapi.AssetRejectReasonDuplicate && r.AssetId != nil:
		e.Status = BulkCheckUploaded
		e.AssetID = r.AssetId
		if r.IsTrashed != nil {
			e.IsTrashed = *r.IsTrashed
		}
	default:
		e.Status = BulkCheckUnsupported
	}
	return e
}
