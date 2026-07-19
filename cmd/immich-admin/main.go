// immich-admin is a CLI for administering an Immich photo server.
package main

import (
	"context"
	"fmt"
	"os"

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
	root := &cli.Command{
		Name:    "immich-admin",
		Usage:   "Administer an Immich photo server",
		Version: fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "config",
				Usage: "path to the YAML config file",
				Value: "config.prod.yaml",
			},
		},
		Commands: []*cli.Command{
			commands.Assets(),
			commands.Users(),
			commands.ClientWorkflow(),
		},
	}

	if err := root.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
