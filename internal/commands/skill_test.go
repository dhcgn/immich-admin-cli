package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

var skillNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func TestSkillFrontmatter(t *testing.T) {
	lines := strings.Split(skillFrontmatter, "\n")
	if lines[0] != "---" || lines[len(lines)-1] != "---" {
		t.Fatalf("frontmatter must start/end with ---, got %q ... %q", lines[0], lines[len(lines)-1])
	}

	var name, description string
	for _, l := range lines[1 : len(lines)-1] {
		if v, ok := strings.CutPrefix(l, "name: "); ok {
			name = v
		}
		if v, ok := strings.CutPrefix(l, "description: "); ok {
			description = v
		}
	}
	if !skillNameRe.MatchString(name) || len(name) > 64 {
		t.Errorf("invalid skill name %q: lowercase alphanumerics/hyphens, ≤64 chars", name)
	}
	if len(description) < 1 || len(description) > 1024 {
		t.Errorf("skill description length = %d, want 1..1024", len(description))
	}
}

func stubSkillTree() []*cli.Command {
	return []*cli.Command{
		{
			Name:  "assets",
			Usage: "Asset operations",
			Commands: []*cli.Command{
				{
					Name:      "info",
					Usage:     "Show information about assets",
					ArgsUsage: "[ASSET_ID ...]",
					Flags: []cli.Flag{
						&cli.StringFlag{Name: "ids-file", Usage: "read IDs from FILE"},
						&cli.BoolFlag{Name: "json", Usage: "print JSON"},
						&cli.StringFlag{Name: "filter", Aliases: []string{"f"}, Usage: "filter", Value: "all"},
						&cli.IntFlag{Name: "limit", Usage: "max", Value: 50},
						&cli.StringFlag{Name: "user", Usage: "who", Required: true},
						&cli.StringSliceFlag{Name: "tag-id", Usage: "repeatable"},
						&cli.FloatFlag{Name: "ratio", Usage: "ratio"},
						&cli.BoolFlag{Name: "hidden", Usage: "unseen", Hidden: true},
					},
				},
				{Name: "help", Usage: "Shows help", Aliases: []string{"h"}},
			},
		},
		{Name: "help", Usage: "Shows help"},
		{Name: skillCommandName, Usage: "Print a skill"},
	}
}

func TestRenderSkillCatalog(t *testing.T) {
	var b strings.Builder
	renderSkillCatalog(&b, stubSkillTree(), "")
	got := b.String()

	for _, want := range []string{
		"## assets",
		"### assets info",
		"Args: `[ASSET_ID ...]`",
		"`--ids-file` (string): read IDs from FILE",
		"`--json` (bool): print JSON",
		"`--filter, -f` (string): filter [default: \"all\"]",
		"`--limit` (int): max [default: 50]",
		"`--user` (string): who [required]",
		"`--tag-id`",
		"repeatable",
		"`--ratio` (float): ratio [default: 0]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("catalog missing %q\n--- got ---\n%s", want, got)
		}
	}
	for _, notWant := range []string{"unseen", "return-agent-skill", "assets info help", "## help"} {
		if strings.Contains(got, notWant) {
			t.Errorf("catalog contains %q, want it skipped\n--- got ---\n%s", notWant, got)
		}
	}
}

func TestBuildSkillText(t *testing.T) {
	got := buildSkillText(stubSkillTree())

	if !strings.HasPrefix(got, "---\nname: immich-admin-cli\n") {
		t.Error("skill text does not start with the frontmatter")
	}
	if !strings.Contains(got, "### assets info") {
		t.Error("skill text missing catalog content")
	}
	for _, want := range []string{"## Best practices", "--dry-run", "--ids-file", "docker logs"} {
		if !strings.Contains(got, want) {
			t.Errorf("skill text missing best-practices marker %q (embed broken?)", want)
		}
	}
	if !strings.HasSuffix(got, "\n") {
		t.Error("skill text does not end with a newline")
	}
}

func TestWriteSkillFile(t *testing.T) {
	text := buildSkillText(stubSkillTree())
	out := filepath.Join(t.TempDir(), "sub", "dir", "SKILL.md")
	if err := writeSkillFile(out, text); err != nil {
		t.Fatalf("writeSkillFile: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if string(raw) != text {
		t.Error("roundtrip mismatch: file content differs from generated text")
	}
}
