// apitable regenerates the "API Coverage" table in README.md from the
// OpenAPI spec. Implemented operations are detected automatically by scanning
// internal/commands/ for the generated client method names (PascalCased
// operationId, with or without the WithResponse suffix).
//
// Run via `go generate ./...` or `go run ./tools/apitable`.
package main

//go:generate go run .

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const (
	beginMarker = "<!-- API-TABLE:BEGIN -->"
	endMarker   = "<!-- API-TABLE:END -->"
)

type specOperation struct {
	OperationId string   `json:"operationId"`
	Tags        []string `json:"tags"`
	Deprecated  bool     `json:"deprecated"`
	State       string   `json:"x-immich-state"`
}

type spec struct {
	Paths map[string]map[string]specOperation `json:"paths"`
}

type row struct {
	Method      string
	Path        string
	OperationId string
	State       string
	Implemented bool
}

var httpMethods = []string{"get", "put", "post", "delete", "patch", "head", "options"}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}

	specData, err := os.ReadFile(filepath.Join(root, "api", "immich-openapi-specs.json"))
	if err != nil {
		return fmt.Errorf("reading spec: %w", err)
	}
	var s spec
	if err := json.Unmarshal(specData, &s); err != nil {
		return fmt.Errorf("parsing spec: %w", err)
	}

	// API calls may live in commands or in client workflows; both count as implemented.
	var commandsSrcParts []string
	for _, dir := range []string{"commands", "workflows"} {
		src, err := readDirGoFiles(filepath.Join(root, "internal", dir))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // package not created yet
			}
			return err
		}
		commandsSrcParts = append(commandsSrcParts, src)
	}
	commandsSrc := strings.Join(commandsSrcParts, "\n")
	genSrc, err := os.ReadFile(filepath.Join(root, "internal", "immichapi", "immichapi.gen.go"))
	if err != nil {
		return fmt.Errorf("reading generated client: %w", err)
	}

	byTag, deprecatedCount, internalCount := collectRows(&s, commandsSrc, string(genSrc))
	md := render(byTag, deprecatedCount, internalCount)

	readmePath := filepath.Join(root, "README.md")
	if err := splice(readmePath, md); err != nil {
		return err
	}
	fmt.Println("README.md API coverage table updated.")
	return nil
}

// repoRoot walks upward from the working directory until it finds go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found upward of working directory")
		}
		dir = parent
	}
}

// readDirGoFiles concatenates all non-test .go files in dir.
func readDirGoFiles(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", dir, err)
	}
	var b strings.Builder
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", name, err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// collectRows filters and sorts the spec operations, grouped by primary tag.
// Deprecated and Internal operations are excluded and only counted.
func collectRows(s *spec, commandsSrc, genSrc string) (map[string][]row, int, int) {
	byTag := make(map[string][]row)
	deprecatedCount, internalCount := 0, 0

	for path, ops := range s.Paths {
		for _, method := range httpMethods {
			op, ok := ops[method]
			if !ok || op.OperationId == "" {
				continue
			}
			switch {
			case op.Deprecated || op.State == "Deprecated" || hasTag(op.Tags, "Deprecated"):
				deprecatedCount++
				continue
			case op.State == "Internal":
				internalCount++
				continue
			}

			// oapi-codegen method names: PascalCased operationId, plus optional
			// WithBody/WithFormdataBody (form-data ops) and WithResponse suffixes.
			goMethod := strings.ToUpper(op.OperationId[:1]) + op.OperationId[1:]
			methodRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(goMethod) + `(WithBody|WithFormdataBody)?(WithResponse)?\(`)
			if !methodRe.MatchString(genSrc) {
				fmt.Fprintf(os.Stderr, "warning: %s: no generated method %s found — naming convention drift?\n",
					op.OperationId, goMethod)
			}
			implemented := methodRe.MatchString(commandsSrc)

			state := op.State
			if state == "" {
				state = "–"
			}
			tag := primaryTag(op.Tags)
			byTag[tag] = append(byTag[tag], row{
				Method:      strings.ToUpper(method),
				Path:        path,
				OperationId: op.OperationId,
				State:       state,
				Implemented: implemented,
			})
		}
	}

	for _, rows := range byTag {
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Path != rows[j].Path {
				return rows[i].Path < rows[j].Path
			}
			return rows[i].Method < rows[j].Method
		})
	}
	return byTag, deprecatedCount, internalCount
}

func hasTag(tags []string, want string) bool {
	return slices.Contains(tags, want)
}

func primaryTag(tags []string) string {
	for _, t := range tags {
		if t != "Deprecated" {
			return t
		}
	}
	return "(untagged)"
}

func render(byTag map[string][]row, deprecatedCount, internalCount int) string {
	tags := make([]string, 0, len(byTag))
	total, implemented := 0, 0
	for tag, rows := range byTag {
		tags = append(tags, tag)
		total += len(rows)
		for _, r := range rows {
			if r.Implemented {
				implemented++
			}
		}
	}
	sort.Strings(tags)

	var b strings.Builder
	fmt.Fprintf(&b, "**%d of %d endpoints implemented** (%d deprecated and %d internal endpoints omitted per project policy).\n",
		implemented, total, deprecatedCount, internalCount)

	for _, tag := range tags {
		rows := byTag[tag]
		tagImplemented := 0
		for _, r := range rows {
			if r.Implemented {
				tagImplemented++
			}
		}
		fmt.Fprintf(&b, "\n<details>\n<summary><b>%s</b> (%d/%d)</summary>\n\n", tag, tagImplemented, len(rows))
		b.WriteString("| Impl | Method | Path | Operation | State |\n")
		b.WriteString("|:----:|--------|------|-----------|-------|\n")
		for _, r := range rows {
			check := ""
			if r.Implemented {
				check = "✅"
			}
			fmt.Fprintf(&b, "| %s | %s | `%s` | `%s` | %s |\n", check, r.Method, r.Path, r.OperationId, r.State)
		}
		b.WriteString("\n</details>\n")
	}
	return b.String()
}

// splice replaces the content between the README markers.
func splice(readmePath, md string) error {
	data, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("reading README: %w", err)
	}
	content := string(data)

	begin := strings.Index(content, beginMarker)
	end := strings.Index(content, endMarker)
	if begin == -1 || end == -1 || end < begin {
		return fmt.Errorf("markers %s / %s not found in %s", beginMarker, endMarker, readmePath)
	}

	updated := content[:begin+len(beginMarker)] + "\n" + md + content[end:]
	if updated == content {
		return nil // already up to date
	}
	return os.WriteFile(readmePath, []byte(updated), 0o644)
}
