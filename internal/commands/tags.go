package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// Tags returns the `tags` command group (spec tag: Tags).
func Tags() *cli.Command {
	return &cli.Command{
		Name:  "tags",
		Usage: "Tag operations",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List all tags (GET /tags)",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "json",
						Usage: "print the raw response as a JSON array",
					},
				},
				Action: tagsList,
			},
			{
				Name:      "get",
				Usage:     "Show one or more tags by ID (GET /tags/{id})",
				ArgsUsage: "[TAG_ID ...]",
				Flags: []cli.Flag{
					idsFileFlag(),
					&cli.BoolFlag{
						Name:  "json",
						Usage: "print the raw responses as a JSON array",
					},
				},
				Action: tagsGet,
			},
			{
				Name:      "delete",
				Usage:     "Delete one or more tags by ID (DELETE /tags/{id})",
				ArgsUsage: "[TAG_ID ...]",
				Flags: []cli.Flag{
					idsFileFlag(),
					&cli.BoolFlag{
						Name:  "dry-run",
						Usage: "print the tags that would be deleted without changing anything",
					},
					&cli.BoolFlag{
						Name:  "yes",
						Usage: "skip the confirmation prompt before deleting tags",
					},
				},
				Action: tagsDelete,
			},
		},
	}
}

func tagsList(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	resp, err := c.API.GetAllTagsWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("calling GET /tags: %w", err)
	}
	if err := client.Check(resp, http.StatusOK); err != nil {
		return fmt.Errorf("GET /tags: %w", err)
	}

	tags := []immichapi.TagResponseDto{}
	if resp.JSON200 != nil {
		tags = *resp.JSON200
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Value < tags[j].Value })

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tags)
	}

	for _, t := range tags {
		printTagLine(t)
	}
	fmt.Printf("%d tag(s)\n", len(tags))
	return nil
}

func tagsGet(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	// Fan out over the single-tag endpoint: continue on per-ID errors and
	// report failures at the end.
	var tags []*immichapi.TagResponseDto
	failures := 0
	for _, id := range ids {
		resp, err := c.API.GetTagByIdWithResponse(ctx, id)
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: tag %s: %v\n", id, err)
			failures++
			continue
		}
		tags = append(tags, resp.JSON200)
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(tags); err != nil {
			return err
		}
	} else {
		for i, t := range tags {
			if i > 0 {
				fmt.Println()
			}
			printTag(t)
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d tags failed", failures, len(ids))
	}
	return nil
}

func tagsDelete(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}

	dryRun := cmd.Bool("dry-run")
	yes := cmd.Bool("yes")

	if dryRun {
		for _, id := range ids {
			fmt.Printf("[dry-run] would delete tag %s\n", id)
		}
		fmt.Printf("[dry-run] %d tag(s) would be deleted\n", len(ids))
		return nil
	}

	if !yes {
		fmt.Printf("This will PERMANENTLY delete %d tag(s) (tags have no trash; deleting a parent also deletes its children).\n", len(ids))
		fmt.Print("Proceed? [y/N]: ")
		if !confirm(os.Stdin) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	// Fan out over the single-tag endpoint: continue on per-ID errors and
	// report failures at the end so the exit code reflects partial failure.
	failures := 0
	for _, id := range ids {
		resp, err := c.API.DeleteTagWithResponse(ctx, id)
		if err == nil {
			err = client.Check(resp, http.StatusNoContent)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: tag %s: %v\n", id, err)
			failures++
			continue
		}
		fmt.Printf("Deleted tag %s\n", id)
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d tags failed", failures, len(ids))
	}
	return nil
}

// printTagLine prints a compact one-line summary of a tag (used by `tags list`).
func printTagLine(t immichapi.TagResponseDto) {
	color := ""
	if t.Color != nil && *t.Color != "" {
		color = "  " + *t.Color
	}
	fmt.Printf("%s  %s%s\n", t.Id, t.Value, color)
}

// printTag prints a multi-line detail view of a tag (used by `tags get`).
func printTag(t *immichapi.TagResponseDto) {
	fmt.Printf("ID:      %s\n", t.Id)
	fmt.Printf("Name:    %s\n", t.Name)
	fmt.Printf("Value:   %s\n", t.Value)
	if t.Color != nil && *t.Color != "" {
		fmt.Printf("Color:   %s\n", *t.Color)
	}
	if t.ParentId != nil && *t.ParentId != "" {
		fmt.Printf("Parent:  %s\n", *t.ParentId)
	}
	fmt.Printf("Created: %s\n", t.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated: %s\n", t.UpdatedAt.Format("2006-01-02 15:04:05"))
}
