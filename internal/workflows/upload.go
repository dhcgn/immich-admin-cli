package workflows

import (
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
	// Progress, when non-nil, counts assetData bytes for the per-file bar.
	// Total/Label should already be set by the caller (total = file size).
	Progress *ByteProgress
}

// DuplicateUploadError is returned when the server matches the upload
// checksum to an existing asset (200) instead of creating one (201).
// Callers that only link duplicates (watch-upload) can errors.As this to
// get the existing ID; callers that must act on their own upload
// (replace-asset) treat it as a plain aborting error.
type DuplicateUploadError struct {
	ExistingID openapi_types.UUID
	HasID      bool
}

func (e *DuplicateUploadError) Error() string {
	id := "unknown"
	if e.HasID {
		id = e.ExistingID.String()
	}
	return fmt.Sprintf("upload was treated as a duplicate of existing asset %s (checksum matches); aborting to avoid acting on the wrong asset", id)
}

// UploadAssetFile uploads the local file at path as a new asset
// (POST /assets, multipart/form-data) and returns its new asset ID.
//
// oapi-codegen only generates a type alias for the multipart body of binary
// fields (UploadAssetMultipartRequestBody = AssetMediaCreateDto); it does not
// generate a multipart writer, so the request body is built by hand here.
//
// The body streams via io.Pipe (never buffered in RAM) so GB videos don't
// OOM and a counting reader reports real upload bytes.
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

	fields := [][2]string{
		{"fileCreatedAt", createdAt},
		{"fileModifiedAt", modifiedAt},
	}
	if opts.Filename != nil {
		fields = append(fields, [2]string{"filename", *opts.Filename})
	}
	if opts.Duration != nil {
		fields = append(fields, [2]string{"duration", strconv.Itoa(*opts.Duration)})
	}
	if opts.IsFavorite != nil {
		fields = append(fields, [2]string{"isFavorite", strconv.FormatBool(*opts.IsFavorite)})
	}
	if opts.Visibility != nil {
		fields = append(fields, [2]string{"visibility", string(*opts.Visibility)})
	}
	if opts.LivePhotoVideoId != nil {
		fields = append(fields, [2]string{"livePhotoVideoId", opts.LivePhotoVideoId.String()})
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	contentType := mw.FormDataContentType()

	// Stream the multipart body: fields + file (+ sidecar) without ever
	// buffering the whole file in RAM. A counting reader reports real
	// upload bytes for the per-file bar.
	writeErr := make(chan error, 1)
	go func() {
		var werr error
		defer func() {
			cerr := mw.Close()
			if werr != nil {
				pw.CloseWithError(werr)
			} else if cerr != nil {
				pw.CloseWithError(fmt.Errorf("building upload body: %w", cerr))
			} else {
				pw.Close()
			}
			writeErr <- werr
		}()
		for _, kv := range fields {
			if err := mw.WriteField(kv[0], kv[1]); err != nil {
				werr = fmt.Errorf("building upload body: %w", err)
				return
			}
		}
		part, err := mw.CreateFormFile("assetData", filepath.Base(path))
		if err != nil {
			werr = fmt.Errorf("building upload body: %w", err)
			return
		}
		src := io.Reader(f)
		if opts.Progress != nil {
			src = opts.Progress.Wrap(f)
		}
		if _, err := io.Copy(part, src); err != nil {
			werr = fmt.Errorf("reading %q: %w", path, err)
			return
		}
		if opts.Progress != nil {
			opts.Progress.Finish()
		}
		if opts.SidecarPath != "" {
			sf, err := os.Open(opts.SidecarPath)
			if err != nil {
				werr = fmt.Errorf("opening sidecar %q: %w", opts.SidecarPath, err)
				return
			}
			defer sf.Close()
			spart, err := mw.CreateFormFile("sidecarData", filepath.Base(opts.SidecarPath))
			if err != nil {
				werr = fmt.Errorf("building upload body: %w", err)
				return
			}
			if _, err := io.Copy(spart, sf); err != nil {
				werr = fmt.Errorf("reading sidecar %q: %w", opts.SidecarPath, err)
				return
			}
		}
	}()

	var params *immichapi.UploadAssetParams
	if opts.Key != nil || opts.Slug != nil || opts.Checksum != nil {
		params = &immichapi.UploadAssetParams{Key: opts.Key, Slug: opts.Slug, XImmichChecksum: opts.Checksum}
	}

	resp, err := c.API.UploadAssetWithBodyWithResponse(ctx, params, contentType, pr)
	if err != nil {
		// Surface a local pipe-build failure instead of a generic upload error.
		select {
		case werr := <-writeErr:
			if werr != nil {
				return openapi_types.UUID{}, werr
			}
		default:
		}
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
		// instead of creating a new one.
		dup := &DuplicateUploadError{}
		if resp.JSON200 != nil {
			dup.ExistingID = resp.JSON200.Id
			dup.HasID = true
		}
		return openapi_types.UUID{}, dup
	default:
		return openapi_types.UUID{}, fmt.Errorf("server returned %s (expected 200 or 201): %s", resp.Status(), string(resp.GetBody()))
	}
}
