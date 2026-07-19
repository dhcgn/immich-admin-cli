package workflows

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// ReplacePair identifies one existing asset to replace and the local file to
// replace it with.
type ReplacePair struct {
	AssetID     openapi_types.UUID
	NewFilePath string
}

// ReplaceAssetOptions controls the replace-asset workflow.
type ReplaceAssetOptions struct {
	// DryRun prints the planned steps without calling the API.
	DryRun bool
	// Force permanently deletes the original instead of trashing it. Only
	// relevant when KeepOriginal is false.
	Force bool
	// KeepOriginal, when true, skips the "remove original" step entirely
	// (it is not added to the step list at all), leaving the old asset
	// completely untouched.
	KeepOriginal bool
}

// ReplaceAsset uploads pair.NewFilePath as a new asset, verifies the upload,
// copies metadata from pair.AssetID onto the new asset, and — unless
// opts.KeepOriginal is set — removes the original asset. It implements the
// `client-workflow replace-asset` steps documented in README.md:
//
//  1. Upload the new file as a new asset
//  2. Verify the upload (asset exists, checksum matches the local file)
//  3. Copy metadata from the old asset (albums, favorite, shared links,
//     sidecar, stack association)
//  4. Remove the old asset (to trash by default; Force for permanent
//     deletion) — only when !opts.KeepOriginal
//
// The destructive step (4) is only ever appended to the step list when it is
// meant to run, and it is always last, so a failure in any earlier step
// leaves the original asset untouched.
func ReplaceAsset(ctx context.Context, c *client.Client, pair ReplacePair, opts ReplaceAssetOptions) error {
	label := pair.AssetID.String()

	var newAssetID openapi_types.UUID

	steps := []Step{
		{
			Name: fmt.Sprintf("Upload new file %s", pair.NewFilePath),
			Run: func(ctx context.Context) error {
				id, err := uploadReplacementAsset(ctx, c, pair.NewFilePath)
				if err != nil {
					return err
				}
				newAssetID = id
				return nil
			},
		},
		{
			Name: "Verify upload (checksum matches local file)",
			Run: func(ctx context.Context) error {
				return verifyUploadedAsset(ctx, c, newAssetID, pair.NewFilePath)
			},
		},
		{
			Name: "Copy metadata from original asset",
			Run: func(ctx context.Context) error {
				return copyAssetMetadata(ctx, c, pair.AssetID, newAssetID)
			},
		},
	}

	if !opts.KeepOriginal {
		steps = append(steps, Step{
			Name: "Remove original asset",
			Run: func(ctx context.Context) error {
				return removeOriginalAsset(ctx, c, pair.AssetID, opts.Force)
			},
		})
	}

	if err := RunSteps(ctx, RunOptions{DryRun: opts.DryRun}, label, steps); err != nil {
		return err
	}

	if !opts.DryRun {
		fmt.Printf("Replaced %s with new asset %s (%s)\n", pair.AssetID, newAssetID, filepath.Base(pair.NewFilePath))
	}
	return nil
}

// uploadReplacementAsset uploads the local file at path as a new asset
// (POST /assets, multipart/form-data) and returns its new asset ID.
//
// oapi-codegen only generates a type alias for the multipart body of binary
// fields (UploadAssetMultipartRequestBody = AssetMediaCreateDto); it does not
// generate a multipart writer, so the request body is built by hand here.
func uploadReplacementAsset(ctx context.Context, c *client.Client, path string) (openapi_types.UUID, error) {
	f, err := os.Open(path)
	if err != nil {
		return openapi_types.UUID{}, fmt.Errorf("opening %q: %w", path, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return openapi_types.UUID{}, fmt.Errorf("stating %q: %w", path, err)
	}
	mtime := fi.ModTime().UTC().Format(time.RFC3339)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	if err := mw.WriteField("fileCreatedAt", mtime); err != nil {
		return openapi_types.UUID{}, fmt.Errorf("building upload body: %w", err)
	}
	if err := mw.WriteField("fileModifiedAt", mtime); err != nil {
		return openapi_types.UUID{}, fmt.Errorf("building upload body: %w", err)
	}
	part, err := mw.CreateFormFile("assetData", filepath.Base(path))
	if err != nil {
		return openapi_types.UUID{}, fmt.Errorf("building upload body: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return openapi_types.UUID{}, fmt.Errorf("reading %q: %w", path, err)
	}
	if err := mw.Close(); err != nil {
		return openapi_types.UUID{}, fmt.Errorf("building upload body: %w", err)
	}

	resp, err := c.API.UploadAssetWithBodyWithResponse(ctx, nil, mw.FormDataContentType(), &body)
	if err != nil {
		return openapi_types.UUID{}, fmt.Errorf("uploading asset: %w", err)
	}

	switch resp.StatusCode() {
	case http.StatusCreated:
		if resp.JSON201 == nil {
			return openapi_types.UUID{}, fmt.Errorf("upload succeeded but response had no body")
		}
		return resp.JSON201.Id, nil
	case http.StatusOK:
		// The server matched the file's checksum to an existing asset
		// instead of creating a new one. Treating that asset as "our"
		// replacement would risk running copy/delete against the wrong
		// (or the same) asset, so abort instead.
		existingID := "unknown"
		if resp.JSON200 != nil {
			existingID = resp.JSON200.Id.String()
		}
		return openapi_types.UUID{}, fmt.Errorf("upload was treated as a duplicate of existing asset %s (checksum matches); aborting to avoid acting on the wrong asset", existingID)
	default:
		return openapi_types.UUID{}, fmt.Errorf("server returned %s (expected 200 or 201): %s", resp.Status(), string(resp.GetBody()))
	}
}

// verifyUploadedAsset confirms the newly uploaded asset exists and its
// server-side checksum matches the local file's SHA1.
func verifyUploadedAsset(ctx context.Context, c *client.Client, newAssetID openapi_types.UUID, path string) error {
	resp, err := c.API.GetAssetInfoWithResponse(ctx, newAssetID, nil)
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return fmt.Errorf("fetching uploaded asset info: %w", err)
	}

	localChecksum, err := fileSHA1Base64(path)
	if err != nil {
		return fmt.Errorf("computing local checksum: %w", err)
	}
	if resp.JSON200.Checksum != localChecksum {
		return fmt.Errorf("checksum mismatch: server has %q, local file has %q", resp.JSON200.Checksum, localChecksum)
	}
	return nil
}

// copyAssetMetadata copies album, favorite, shared-link, sidecar, and stack
// association from sourceID to targetID via PUT /assets/copy.
func copyAssetMetadata(ctx context.Context, c *client.Client, sourceID, targetID openapi_types.UUID) error {
	resp, err := c.API.CopyAssetWithResponse(ctx, immichapi.AssetCopyDto{
		SourceId: sourceID,
		TargetId: targetID,
	})
	if err == nil {
		err = client.Check(resp, http.StatusNoContent)
	}
	if err != nil {
		return fmt.Errorf("copying asset metadata: %w", err)
	}
	return nil
}

// removeOriginalAsset deletes the original asset (DELETE /assets), trashing
// it by default or permanently deleting it when force is true.
func removeOriginalAsset(ctx context.Context, c *client.Client, id openapi_types.UUID, force bool) error {
	resp, err := c.API.DeleteAssetsWithResponse(ctx, immichapi.AssetBulkDeleteDto{
		Ids:   []openapi_types.UUID{id},
		Force: &force,
	})
	if err == nil {
		err = client.Check(resp, http.StatusNoContent)
	}
	if err != nil {
		return fmt.Errorf("removing original asset: %w", err)
	}
	return nil
}

// fileSHA1Base64 returns the base64-standard-encoded SHA1 hash of the file
// at path, matching the format Immich reports in AssetResponseDto.Checksum.
func fileSHA1Base64(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}
