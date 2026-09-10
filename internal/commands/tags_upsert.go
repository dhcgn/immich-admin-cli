package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// tagsUpsertCommand exposes PUT /tags (upsertTags).
func tagsUpsertCommand() *cli.Command {
	return &cli.Command{
		Name:      "upsert",
		Usage:     "Create tags (and missing parents) by full path, or return existing ones (PUT /tags)",
		ArgsUsage: "[TAG_VALUE ...]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "json", Usage: "print the raw response as a JSON array"},
		},
		Action: tagsUpsert,
	}
}

func tagsUpsert(ctx context.Context, cmd *cli.Command) error {
	values := cmd.Args().Slice()
	if len(values) == 0 {
		return fmt.Errorf("no tag values given: pass them as arguments (e.g. \"Travel/2024\")")
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	resp, err := c.API.UpsertTagsWithResponse(ctx, immichapi.TagUpsertDto{Tags: values})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return fmt.Errorf("upserting tags: %w", err)
	}
	tags := []immichapi.TagResponseDto{}
	if resp.JSON200 != nil {
		tags = *resp.JSON200
	}

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
