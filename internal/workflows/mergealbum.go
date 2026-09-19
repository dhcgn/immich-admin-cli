package workflows

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"slices"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// MergeAlbumOptions controls the merge-album workflow: every asset in From
// is added to Into, then removed from From; optionally the emptied source
// album is deleted afterwards.
type MergeAlbumOptions struct {
	// From is the source album: assets move out of it.
	From openapi_types.UUID
	// Into is the target album: assets move into it.
	Into openapi_types.UUID
	// DeleteEmptySource deletes the source album when it holds no assets
	// after the move (never deletes a non-empty album).
	DeleteEmptySource bool
	// DryRun prints the merge plan without calling any mutating endpoint.
	DryRun bool
}

// MergeAlbums moves every asset from opts.From to opts.Into (PUT then
// DELETE /albums/{id}/assets, 500 IDs per request) and, when
// DeleteEmptySource is set and the move left nothing behind, deletes the
// source album (DELETE /albums/{id}). Only asset IDs confirmed present in
// the target (added, or already there) are removed from the source, so a
// failed add never loses an asset; the destructive delete is always last.
// Failures are logged to stderr and summarized in the returned error.
func MergeAlbums(ctx context.Context, c *client.Client, opts MergeAlbumOptions) error {
	if opts.From == opts.Into {
		return fmt.Errorf("cannot merge album %s into itself: --from and --into must differ", opts.From)
	}

	from, err := ResolveAlbum(ctx, c, &opts.From, "")
	if err != nil {
		return err
	}
	into, err := ResolveAlbum(ctx, c, &opts.Into, "")
	if err != nil {
		return err
	}

	assets, err := fetchAlbumAssets(ctx, c, opts.From)
	if err != nil {
		return err
	}
	ids := make([]openapi_types.UUID, 0, len(assets))
	for _, a := range assets {
		ids = append(ids, a.Id)
	}

	if opts.DryRun {
		fmt.Printf("[dry-run] would move %d asset(s) from album %s %q to album %s %q\n",
			len(ids), from.Id, from.AlbumName, into.Id, into.AlbumName)
		if opts.DeleteEmptySource {
			fmt.Printf("[dry-run] would delete the source album %s once empty\n", from.Id)
		}
		return nil
	}

	if len(ids) == 0 {
		fmt.Printf("Source album %s %q holds no assets; nothing to move.\n", from.Id, from.AlbumName)
	} else {
		fmt.Printf("Moving %d asset(s) from album %s %q to album %s %q...\n",
			len(ids), from.Id, from.AlbumName, into.Id, into.AlbumName)
	}

	var failures []string

	// 1. Add everything to the target. Only IDs known present there
	// afterwards (added, or already there) are removed from the source.
	var present []openapi_types.UUID
	for chunk := range slices.Chunk(ids, 500) {
		resp, err := c.API.AddAssetsToAlbumWithResponse(ctx, opts.Into, immichapi.BulkIdsDto{Ids: chunk})
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("adding %d asset(s) to %s: %v", len(chunk), into.Id, err))
			continue
		}
		if resp.JSON200 == nil {
			failures = append(failures, fmt.Sprintf("adding %d asset(s) to %s: response had no body", len(chunk), into.Id))
			continue
		}
		for _, r := range *resp.JSON200 {
			switch {
			case r.Success, r.Error != nil && *r.Error == immichapi.BulkIdErrorReasonDuplicate:
				present = append(present, r.Id)
			default:
				failures = append(failures, fmt.Sprintf("asset %s: %s", r.Id, bulkFailureReason(r)))
			}
		}
	}
	fmt.Printf("%d of %d asset(s) now in target album %s\n", len(present), len(ids), into.Id)

	// 2. Remove the moved assets from the source.
	removed := 0
	for chunk := range slices.Chunk(present, 500) {
		resp, err := c.API.RemoveAssetFromAlbumWithResponse(ctx, opts.From, immichapi.BulkIdsDto{Ids: chunk})
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("removing %d asset(s) from %s: %v", len(chunk), from.Id, err))
			continue
		}
		if resp.JSON200 == nil {
			failures = append(failures, fmt.Sprintf("removing %d asset(s) from %s: response had no body", len(chunk), from.Id))
			continue
		}
		for _, r := range *resp.JSON200 {
			if r.Success {
				removed++
			} else {
				failures = append(failures, fmt.Sprintf("asset %s: %s", r.Id, bulkFailureReason(r)))
			}
		}
	}
	fmt.Printf("%d asset(s) removed from source album %s\n", removed, from.Id)

	// 3. Destructive step, last: delete the source only when the move left
	// it empty and nothing failed.
	if opts.DeleteEmptySource && len(failures) == 0 {
		info, err := c.API.GetAlbumInfoWithResponse(ctx, opts.From, &immichapi.GetAlbumInfoParams{})
		if err == nil {
			err = client.Check(info, http.StatusOK)
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("re-reading source album %s: %v", from.Id, err))
		} else if info.JSON200 == nil {
			failures = append(failures, fmt.Sprintf("re-reading source album %s: response had no body", from.Id))
		} else if info.JSON200.AssetCount == 0 {
			del, err := c.API.DeleteAlbumWithResponse(ctx, opts.From)
			if err == nil {
				err = client.Check(del, http.StatusNoContent)
			}
			if err != nil {
				failures = append(failures, fmt.Sprintf("deleting source album %s: %v", from.Id, err))
			} else {
				fmt.Printf("Deleted emptied source album %s\n", from.Id)
			}
		} else {
			fmt.Printf("Source album %s still holds %d asset(s); keeping it.\n", from.Id, info.JSON200.AssetCount)
		}
	}

	for _, f := range failures {
		fmt.Fprintf(os.Stderr, "Error: %s\n", f)
	}
	if len(failures) > 0 {
		return fmt.Errorf("merge incomplete: %d failure(s) moving %d asset(s) from %s to %s", len(failures), len(ids), from.Id, into.Id)
	}
	return nil
}

// bulkFailureReason renders one failed BulkIdResponseDto's reason,
// preferring the server's message. Small and local (the commands package
// has its own bulkIDError for the same shape) so workflows stays
// independent of internal/commands.
func bulkFailureReason(r immichapi.BulkIdResponseDto) string {
	if r.ErrorMessage != nil && *r.ErrorMessage != "" {
		return *r.ErrorMessage
	}
	if r.Error != nil && *r.Error != "" {
		return string(*r.Error)
	}
	return "server reported failure"
}
