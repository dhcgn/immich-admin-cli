// Package commands defines the CLI command tree. One file per OpenAPI spec
// tag; commands are grouped as `<tag> <operation>` (e.g. `users me`).
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/client"
)

// Users returns the `users` command group (spec tag: Users).
func Users() *cli.Command {
	return &cli.Command{
		Name:  "users",
		Usage: "User operations",
		Commands: []*cli.Command{
			{
				Name:  "me",
				Usage: "Show the user that owns the API key (GET /users/me)",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "json",
						Usage: "print the raw response as JSON",
					},
				},
				Action: usersMe,
			},
		},
	}
}

func usersMe(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(cmd)
	if err != nil {
		return err
	}

	resp, err := c.API.GetMyUserWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("calling GET /users/me: %w", err)
	}
	if err := client.Check(resp, http.StatusOK); err != nil {
		return fmt.Errorf("GET /users/me: %w", err)
	}
	user := resp.JSON200

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(user)
	}

	fmt.Printf("Name:     %s\n", user.Name)
	fmt.Printf("Email:    %s\n", user.Email)
	fmt.Printf("ID:       %s\n", user.Id)
	fmt.Printf("Admin:    %t\n", user.IsAdmin)
	fmt.Printf("Status:   %s\n", user.Status)
	fmt.Printf("Storage:  %s used of %s\n",
		formatBytes(user.QuotaUsageInBytes), formatBytes(user.QuotaSizeInBytes))
	return nil
}
