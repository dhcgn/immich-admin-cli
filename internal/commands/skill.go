package commands

//go:generate go run ../../cmd/immich-admin return-agent-skill --out ../../immich-admin-cli/SKILL.md

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
)

//go:embed skill_best_practices.md
var skillBestPractices string

// skillCommandName is the top-level command that prints this skill. It is
// skipped in the generated catalog (a skill describing how to print itself
// helps nobody).
const skillCommandName = "return-agent-skill"

// skillFrontmatter is the Agent Skills frontmatter for this CLI. The name
// must stay in sync with the directory agents save the output to
// (immich-admin-cli/SKILL.md): lowercase alphanumerics/hyphens, ≤64 chars;
// description ≤1024 chars (both enforced by skill_test.go).
const skillFrontmatter = `---
name: immich-admin-cli
description: Administer an Immich photo server from the command line: manage assets, albums, tags, and users; search by metadata; download originals and thumbnails; upload files; inspect server workflows and their run logs; and run client-side bulk workflows that find and repair corrupt photos, replace assets, re-encode media, and mirror albums locally. Use when working with Immich photo libraries, thumbnails, metadata, bulk tagging, or photo backup automation.
compatibility: Requires the immich-admin CLI binary and network access to an Immich server v3.2.0 or newer.
---`

// Skill returns the `return-agent-skill` top-level command: it prints a
// SKILL.md agent skill for this CLI (frontmatter, a command catalog
// generated from the live command tree, and best practices). Fully offline:
// it never contacts the server and needs no config.
func Skill() *cli.Command {
	return &cli.Command{
		Name:  skillCommandName,
		Usage: "Print a SKILL.md agent skill for this CLI (commands, workflows, best practices)",
		Description: "Generates an Agent Skills SKILL.md for immich-admin: frontmatter, " +
			"the full command catalog with flags, and best-practice patterns. Save the output " +
			"as immich-admin-cli/SKILL.md to use it as an agent skill " +
			"(`go generate ./...` refreshes that checked-in copy automatically). " +
			"Fully offline: needs no server connection or config file.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "out",
				Usage: "write SKILL.md to `FILE` instead of stdout (parent directories are created)",
			},
		},
		Action: skillAction,
	}
}

func skillAction(_ context.Context, cmd *cli.Command) error {
	text := buildSkillText(cmd.Root().Commands)

	if out := cmd.String("out"); out != "" {
		if err := writeSkillFile(out, text); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Wrote %s (%d lines)\n", out, strings.Count(text, "\n"))
		return nil
	}

	fmt.Print(text)
	return nil
}

// writeSkillFile writes text to out, creating parent directories as needed.
func writeSkillFile(out, text string) error {
	if dir := filepath.Dir(out); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating output directory %q: %w", dir, err)
		}
	}
	if err := os.WriteFile(out, []byte(text), 0o644); err != nil {
		return fmt.Errorf("writing output file %q: %w", out, err)
	}
	return nil
}

// buildSkillText assembles the complete SKILL.md: frontmatter, a short
// intro, the command catalog generated from cmds, and the embedded best
// practices. Pure (no I/O, no network) so the output is directly
// unit-testable.
func buildSkillText(cmds []*cli.Command) string {
	var b strings.Builder
	b.WriteString(skillFrontmatter)
	b.WriteString("\n\nThe `immich-admin` CLI administers an Immich photo server from the command " +
		"line. Configure it with `--config FILE` (default `config.prod.yaml`) or the " +
		"`IMMICH_SERVER` / `IMMICH_API_KEY` env vars; the server must be v3.2.0 or newer.\n")
	b.WriteString("\n## Commands\n\nRun `immich-admin <command path> --help` for full details and examples.\n\n")
	renderSkillCatalog(&b, cmds, "")
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(skillBestPractices))
	b.WriteString("\n")
	return b.String()
}

// renderSkillCatalog appends one section per command (groups recurse with
// their subcommands). Only name, usage, args, and flags are rendered — long
// descriptions stay in `--help` to keep the skill compact.
func renderSkillCatalog(b *strings.Builder, cmds []*cli.Command, parent string) {
	for _, c := range cmds {
		if c.Name == skillCommandName || c.Name == "help" {
			// Skip this command itself and urfave/cli's auto-generated
			// `help` subcommands (present on every group after setup —
			// without this, every leaf looks like a group).
			continue
		}
		path := c.Name
		if parent != "" {
			path = parent + " " + c.Name
		}
		if subs := skillSubcommands(c.Commands); len(subs) > 0 {
			fmt.Fprintf(b, "## %s\n\n%s\n\n", path, c.Usage)
			renderSkillCatalog(b, subs, path)
			continue
		}
		if len(c.Aliases) > 0 {
			fmt.Fprintf(b, "### %s (alias: %s)\n\n%s\n", path, strings.Join(c.Aliases, ", "), c.Usage)
		} else {
			fmt.Fprintf(b, "### %s\n\n%s\n", path, c.Usage)
		}
		if c.ArgsUsage != "" {
			fmt.Fprintf(b, "Args: `%s`\n", c.ArgsUsage)
		}
		for _, f := range c.Flags {
			if line := renderSkillFlag(f); line != "" {
				b.WriteString(line)
				b.WriteByte('\n')
			}
		}
		b.WriteByte('\n')
	}
}

// skillSubcommands returns the real subcommands, dropping urfave/cli's
// auto-generated `help` entries (see renderSkillCatalog).
func skillSubcommands(cmds []*cli.Command) []*cli.Command {
	var subs []*cli.Command
	for _, c := range cmds {
		if c.Name == "help" {
			continue
		}
		subs = append(subs, c)
	}
	return subs
}

// skillFlag is the subset of the urfave/cli flag API needed to render one
// flag line. Every flag type used by this CLI implements it.
type skillFlag interface {
	cli.Flag
	Names() []string
	GetUsage() string
	GetValue() string
	IsRequired() bool
	IsVisible() bool
	TypeName() string
}

// renderSkillFlag renders one flag as `- --name, -a (type): usage
// [default: X] [required]`. Hidden flags are skipped (empty string).
func renderSkillFlag(f cli.Flag) string {
	sf, ok := f.(skillFlag)
	if !ok {
		return fmt.Sprintf("- `%v`", f)
	}
	if !sf.IsVisible() {
		return ""
	}
	if len(sf.Names()) > 0 && sf.Names()[0] == "help" {
		return "" // urfave/cli's auto-added --help flag: universal noise
	}
	names := make([]string, 0, len(sf.Names()))
	for _, n := range sf.Names() {
		if len(n) == 1 {
			names = append(names, "-"+n)
		} else {
			names = append(names, "--"+n)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- `%s`", strings.Join(names, ", "))
	if t := sf.TypeName(); t != "" {
		fmt.Fprintf(&b, " (%s)", t)
	}
	if u := sf.GetUsage(); u != "" {
		b.WriteString(": " + u)
	}
	if sf.IsRequired() {
		b.WriteString(" [required]")
	} else if d := sf.GetValue(); d != "" && d != "false" {
		fmt.Fprintf(&b, " [default: %s]", d)
	}
	return b.String()
}
