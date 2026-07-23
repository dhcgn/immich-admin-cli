package workflows

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// TagDeleteOptions controls the tag-delete workflow.
type TagDeleteOptions struct {
	// Include, when non-nil, keeps only tags whose Value (full hierarchical
	// path) matches it. A nil Include matches every tag.
	Include *regexp.Regexp
	// Exclude, when non-nil, drops any tag whose Value matches it, even if it
	// matched Include. A nil Exclude excludes nothing.
	Exclude *regexp.Regexp
	// DryRun prints the planned delete steps without calling the API.
	DryRun bool
}

// SelectTagsForDeletion fetches all tags (GET /tags) and returns those whose
// Value (full path) matches opts.Include and does not match opts.Exclude,
// sorted by Value for deterministic output.
//
// It performs no deletion — this is the read-only "getting the information"
// path used both by the command's display step and by the integration test.
func SelectTagsForDeletion(ctx context.Context, c *client.Client, opts TagDeleteOptions) ([]immichapi.TagResponseDto, error) {
	resp, err := c.API.GetAllTagsWithResponse(ctx)
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching tags: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("fetching tags: response had no body")
	}

	return filterTags(*resp.JSON200, opts), nil
}

// filterTags returns the tags whose Value matches opts.Include and does not
// match opts.Exclude, sorted by Value. It is pure (no network) so the
// selection logic can be unit-tested in isolation.
func filterTags(tags []immichapi.TagResponseDto, opts TagDeleteOptions) []immichapi.TagResponseDto {
	var matched []immichapi.TagResponseDto
	for _, tag := range tags {
		if opts.Include != nil && !opts.Include.MatchString(tag.Value) {
			continue
		}
		if opts.Exclude != nil && opts.Exclude.MatchString(tag.Value) {
			continue
		}
		matched = append(matched, tag)
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Value < matched[j].Value
	})
	return matched
}

// DeleteTags deletes each tag (DELETE /tags/{id}) as a single destructive step
// per tag, continuing on error and returning a summary error if any deletion
// failed (the same bulk convention used by the other workflows). In dry-run
// mode it prints the planned step for each tag and deletes nothing.
//
// Tag deletion is permanent: the Tags API has no trash, so there is no
// restore step and no --force distinction.
func DeleteTags(ctx context.Context, c *client.Client, tags []immichapi.TagResponseDto, opts TagDeleteOptions) error {
	return RunBatch(tags,
		func(t immichapi.TagResponseDto) string { return t.Value },
		func(t immichapi.TagResponseDto) error {
			steps := []Step{
				{
					Name: fmt.Sprintf("Delete tag %q (%s)", t.Value, t.Id),
					Run: func(ctx context.Context) error {
						return deleteTag(ctx, c, t.Id)
					},
				},
			}
			return RunSteps(ctx, RunOptions{DryRun: opts.DryRun}, t.Value, steps)
		},
	)
}

// deleteTag permanently deletes a single tag by ID (DELETE /tags/{id}).
func deleteTag(ctx context.Context, c *client.Client, id openapi_types.UUID) error {
	resp, err := c.API.DeleteTagWithResponse(ctx, id)
	if err == nil {
		err = client.Check(resp, http.StatusNoContent)
	}
	if err != nil {
		return fmt.Errorf("deleting tag: %w", err)
	}
	return nil
}
