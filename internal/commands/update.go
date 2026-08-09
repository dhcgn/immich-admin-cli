package commands

import (
	"context"
	"fmt"
	"os"
	"runtime"

	ghupdate "github.com/dhcgn/gh-update"
	"github.com/urfave/cli/v3"
)

// updateRepo is the GitHub repository releases are checked/downloaded from.
const updateRepo = "dhcgn/immich-admin-cli"

// Update returns the `update` command: it checks the latest GitHub release
// for a build matching the current OS/arch and, on confirmation, replaces
// the running executable in place and restarts it.
//
// Release assets are named "immich-admin_<os>_<arch>[.exe]" with no version
// in the filename (see .github/workflows/release.yml) — embedding the
// version there would go stale the moment the binary updates itself in
// place, since the file keeps its original name.
func Update(version string) *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Check for and install the latest release from GitHub",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "check", Usage: "only check for a new version, don't install it"},
			&cli.BoolFlag{Name: "yes", Usage: "install without a confirmation prompt"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			assetFilter := fmt.Sprintf(`^immich-admin_%s_%s(\.exe)?$`, runtime.GOOS, runtime.GOARCH)

			latest, err := ghupdate.GetLatestVersion(updateRepo, version, assetFilter)
			if err != nil {
				if err == ghupdate.ErrorNoNewVersionFound {
					fmt.Printf("Already running the latest version (%s).\n", version)
					return nil
				}
				return fmt.Errorf("checking for update: %w", err)
			}

			fmt.Printf("New version available: %s -> %s\n", version, latest.Version)
			if cmd.Bool("check") {
				return nil
			}

			if !cmd.Bool("yes") {
				fmt.Print("Install and restart now? [y/N]: ")
				if !confirm(os.Stdin) {
					fmt.Println("Aborted.")
					return nil
				}
			}

			exePath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locating running executable: %w", err)
			}

			if err := ghupdate.SelfUpdateAndRestart(latest, exePath); err != nil {
				return fmt.Errorf("installing update: %w", err)
			}

			fmt.Println("Update installed, restarting...")
			return nil
		},
	}
}
