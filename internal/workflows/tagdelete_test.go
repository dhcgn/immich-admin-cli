package workflows

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func tag(value string) immichapi.TagResponseDto {
	return immichapi.TagResponseDto{Name: value, Value: value}
}

func values(tags []immichapi.TagResponseDto) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Value)
	}
	return out
}

func TestFilterTags(t *testing.T) {
	all := []immichapi.TagResponseDto{
		tag("Travel/2024"),
		tag("Travel/2023"),
		tag("Family"),
		tag("Work/Reports"),
		tag("archive"),
	}

	tests := []struct {
		name    string
		include string
		exclude string
		want    []string
	}{
		{
			name: "no filters matches all, sorted by value",
			want: []string{"Family", "Travel/2023", "Travel/2024", "Work/Reports", "archive"},
		},
		{
			name:    "include only",
			include: "^Travel/",
			want:    []string{"Travel/2023", "Travel/2024"},
		},
		{
			name:    "include with exclude narrowing",
			include: "^Travel/",
			exclude: "2023$",
			want:    []string{"Travel/2024"},
		},
		{
			name:    "exclude only",
			exclude: "^Travel/",
			want:    []string{"Family", "Work/Reports", "archive"},
		},
		{
			name:    "no match",
			include: "^Nonexistent",
			want:    []string{},
		},
		{
			name:    "case sensitive by default",
			include: "family",
			want:    []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := TagDeleteOptions{}
			if tc.include != "" {
				opts.Include = regexp.MustCompile(tc.include)
			}
			if tc.exclude != "" {
				opts.Exclude = regexp.MustCompile(tc.exclude)
			}

			got := values(filterTags(all, opts))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("filterTags() = %v, want %v", got, tc.want)
			}
		})
	}
}
