package workflows

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// UploadOptions controls UploadAssetFile. Nil pointers leave the field unset
// (server default), except the timestamps which fall back to the file's mtime.
type UploadOptions struct {
	FileCreatedAt    *time.Time
	FileModifiedAt   *time.Time
	Filename         *string
	Duration         *int
	IsFavorite       *bool
	Visibility       *immichapi.AssetVisibility
	LivePhotoVideoId *openapi_types.UUID
	// SidecarPath, when non-empty, is sent as the sidecarData part.
	SidecarPath string
	Key         *string
	Slug        *string
	Checksum    *string
}

// UploadAssetFile uploads the local file at path as a new asset
// (POST /assets, multipart/form-data) and returns its new asset ID.
//
// oapi-codegen only generates a type alias for the multipart body of binary
// fields (UploadAssetMultipartRequestBody = AssetMediaCreateDto); it does not
// generate a multipart writer, so the request body is built by hand here.
func UploadAssetFile(ctx context.Context, c *client.Client, path string, opts UploadOptions) (openapi_types.UUID, error) {
	f, err := os.Open(path)
	if err != nil {
		return openapi_types.UUID{}, fmt.Errorf("opening %q: %w", path, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return openapi_types.UUID{}, fmt.Errorf("stating %q: %w", path, err)
	}
	mtime := fi.ModTime().UTC()
	createdAt := mtime.Format(time.RFC3339)
	if opts.FileCreatedAt != nil {
		createdAt = opts.FileCreatedAt.UTC().Format(time.RFC3339)
	}
	modifiedAt := mtime.Format(time.RFC3339)
	if opts.FileModifiedAt != nil {
		modifiedAt = opts.FileModifiedAt.UTC().Format(time.RFC3339)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	writeField := func(k, v string) error {
		if err := mw.WriteField(k, v); err != nil {
			return fmt.Errorf("building upload body: %w", err)
		}
		return nil
	}
	if err := writeField("fileCreatedAt", createdAt); err != nil {
		return openapi_types.UUID{}, err
	}
	if err := writeField("fileModifiedAt", modifiedAt); err != nil {
		return openapi_types.UUID{}, err
	}
	if opts.Filename != nil {
		if err := writeField("filename", *opts.Filename); err != nil {
			return openapi_types.UUID{}, err
		}
	}
	if opts.Duration != nil {
		if err := writeField("duration", strconv.Itoa(*opts.Duration)); err != nil {
			return openapi_types.UUID{}, err
		}
	}
	if opts.IsFavorite != nil {
		if err := writeField("isFavorite", strconv.FormatBool(*opts.IsFavorite)); err != nil {
			return openapi_types.UUID{}, err
		}
	}
	if opts.Visibility != nil {
		if err := writeField("visibility", string(*opts.Visibility)); err != nil {
			return openapi_types.UUID{}, err
		}
	}
	if opts.LivePhotoVideoId != nil {
		if err := writeField("livePhotoVideoId", opts.LivePhotoVideoId.String()); err != nil {
			return openapi_types.UUID{}, err
		}
	}
	part, err := mw.CreateFormFile("assetData", filepath.Base(path))
	if err != nil {
		return openapi_types.UUID{}, fmt.Errorf("building upload body: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return openapi_types.UUID{}, fmt.Errorf("reading %q: %w", path, err)
	}
	if opts.SidecarPath != "" {
		sf, err := os.Open(opts.SidecarPath)
		if err != nil {
			return openapi_types.UUID{}, fmt.Errorf("opening sidecar %q: %w", opts.SidecarPath, err)
		}
		defer sf.Close()
		spart, err := mw.CreateFormFile("sidecarData", filepath.Base(opts.SidecarPath))
		if err != nil {
			return openapi_types.UUID{}, fmt.Errorf("building upload body: %w", err)
		}
		if _, err := io.Copy(spart, sf); err != nil {
			return openapi_types.UUID{}, fmt.Errorf("reading sidecar %q: %w", opts.SidecarPath, err)
		}
	}
	if err := mw.Close(); err != nil {
		return openapi_types.UUID{}, fmt.Errorf("building upload body: %w", err)
	}

	var params *immichapi.UploadAssetParams
	if opts.Key != nil || opts.Slug != nil || opts.Checksum != nil {
		params = &immichapi.UploadAssetParams{Key: opts.Key, Slug: opts.Slug, XImmichChecksum: opts.Checksum}
	}

	resp, err := c.API.UploadAssetWithBodyWithResponse(ctx, params, mw.FormDataContentType(), &body)
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
