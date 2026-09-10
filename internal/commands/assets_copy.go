package commands

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// assetsCopyCommand exposes PUT /assets/copy (copyAsset).
func assetsCopyCommand() *cli.Command {
	return &cli.Command{
		Name:      "copy",
		Usage:     "Copy metadata from one asset to another (PUT /assets/copy)",
		ArgsUsage: "SOURCE_ID TARGET_ID",
		Description: "Copies album, favorite, shared-link, sidecar, and stack association " +
			"from SOURCE_ID onto TARGET_ID. Each --flag is optional; unset leaves the server default.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "albums", Usage: "copy album associations: true or false"},
			&cli.StringFlag{Name: "favorite", Usage: "copy favorite status: true or false"},
			&cli.StringFlag{Name: "shared-links", Usage: "copy shared links: true or false"},
			&cli.StringFlag{Name: "sidecar", Usage: "copy sidecar file: true or false"},
			&cli.StringFlag{Name: "stack", Usage: "copy stack association: true or false"},
		},
		Action: assetsCopy,
	}
}

func assetsCopy(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) != 2 {
		return fmt.Errorf("expected exactly 2 positional arguments (SOURCE_ID TARGET_ID), got %d", len(args))
	}
	source, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid SOURCE_ID %q: %w", args[0], err)
	}
	target, err := uuid.Parse(args[1])
	if err != nil {
		return fmt.Errorf("invalid TARGET_ID %q: %w", args[1], err)
	}

	body, err := buildCopyAssetDto(cmd, openapi_types.UUID(source), openapi_types.UUID(target))
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	resp, err := c.API.CopyAssetWithResponse(ctx, *body)
	if err == nil {
		err = client.Check(resp, http.StatusNoContent)
	}
	if err != nil {
		return fmt.Errorf("copying asset metadata: %w", err)
	}
	fmt.Printf("%s -> %s: copied\n", source, target)
	return nil
}

// buildCopyAssetDto maps CLI flags to the API request body. Every copy flag
// is optional (nil leaves the server default).
func buildCopyAssetDto(cmd *cli.Command, source, target openapi_types.UUID) (*immichapi.AssetCopyDto, error) {
	body := &immichapi.AssetCopyDto{SourceId: source, TargetId: target}
	if err := setBoolFlag(cmd, "albums", func(b bool) { body.Albums = &b }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "favorite", func(b bool) { body.Favorite = &b }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "shared-links", func(b bool) { body.SharedLinks = &b }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "sidecar", func(b bool) { body.Sidecar = &b }); err != nil {
		return nil, err
	}
	if err := setBoolFlag(cmd, "stack", func(b bool) { body.Stack = &b }); err != nil {
		return nil, err
	}
	return body, nil
}
