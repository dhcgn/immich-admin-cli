// immich-admin is a CLI for administering an Immich photo server.
package main

import (
	"context"
	"fmt"
	"os"

	ghupdate "github.com/dhcgn/gh-update"
	"github.com/urfave/cli/v3"

	"github.com/dhcgn/immich-admin-cli/internal/commands"
)

// Set at build time via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// After `update` replaces the executable in place, gh-update restarts it
	// with FINISH_UPDATE=1 and the old PID set. This new process cleans up
	// the old binary's ".old" backup file before doing anything else.
	if ghupdate.IsFirstStartAfterUpdate() {
		oldPid := ghupdate.GetOldPid()
		if oldPid != fmt.Sprint(os.Getpid()) {
			if err := ghupdate.CleanUpAfterUpdate(os.Args[0], oldPid); err != nil {
				fmt.Fprintln(os.Stderr, "Warning: failed to clean up previous version:", err)
			}
		}
		fmt.Printf("Updated to %s.\n", version)
	}

	root := &cli.Command{
		Name:    "immich-admin",
		Usage:   "Administer an Immich photo server",
		Version: fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
		CommandNotFound: func(_ context.Context, cmd *cli.Command, name string) {
			fmt.Fprintf(os.Stderr, "Unknown command %q. Run '%s --help' to see available commands.\n", name, cmd.FullName())
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "config",
				Usage: "path to the YAML config file",
				Value: "config.prod.yaml",
			},
		},
		Commands: []*cli.Command{
			commands.Assets(),
			commands.Albums(),
			commands.Search(),
			commands.Tags(),
			commands.Users(),
			commands.ClientWorkflow(),
			commands.Update(version),
		},
	}

	if err := root.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
