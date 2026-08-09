// Package commands defines the CLI command tree. One file per OpenAPI spec
// tag; commands are grouped as `<tag> <operation>` (e.g. `users me`).
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
			{
				Name:  "list",
				Usage: "List all users on the server (GET /users)",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "json",
						Usage: "print the raw response as a JSON array",
					},
				},
				Action: usersList,
			},
			{
				Name:      "get",
				Usage:     "Show one or more users by ID (GET /users/{id})",
				ArgsUsage: "[USER_ID ...]",
				Flags: []cli.Flag{
					idsFileFlag(),
					&cli.BoolFlag{
						Name:  "json",
						Usage: "print the raw responses as a JSON array",
					},
				},
				Action: usersGet,
			},
		},
	}
}

func usersList(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	resp, err := c.API.SearchUsersWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("calling GET /users: %w", err)
	}
	if err := client.Check(resp, http.StatusOK); err != nil {
		return fmt.Errorf("GET /users: %w", err)
	}

	users := []immichapi.UserResponseDto{}
	if resp.JSON200 != nil {
		users = *resp.JSON200
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Name < users[j].Name })

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(users)
	}

	for _, u := range users {
		fmt.Printf("%s  %s  <%s>\n", u.Id, u.Name, u.Email)
	}
	fmt.Printf("%d user(s)\n", len(users))
	return nil
}

func usersGet(ctx context.Context, cmd *cli.Command) error {
	ids, err := collectIDs(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	// Fan out over the single-user endpoint: continue on per-ID errors and
	// report failures at the end.
	var users []*immichapi.UserResponseDto
	failures := 0
	for _, id := range ids {
		resp, err := c.API.GetUserWithResponse(ctx, id)
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: user %s: %v\n", id, err)
			failures++
			continue
		}
		users = append(users, resp.JSON200)
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(users); err != nil {
			return err
		}
	} else {
		for _, u := range users {
			fmt.Printf("ID:      %s\n", u.Id)
			fmt.Printf("Name:    %s\n", u.Name)
			fmt.Printf("Email:   %s\n", u.Email)
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d users failed", failures, len(ids))
	}
	return nil
}

func usersMe(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
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
