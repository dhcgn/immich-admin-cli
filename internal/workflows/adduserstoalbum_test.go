package workflows

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func album(name string) immichapi.AlbumResponseDto {
	return immichapi.AlbumResponseDto{AlbumName: name}
}

func albumNames(albums []immichapi.AlbumResponseDto) []string {
	out := make([]string, 0, len(albums))
	for _, a := range albums {
		out = append(out, a.AlbumName)
	}
	return out
}

func TestFilterAlbums(t *testing.T) {
	all := []immichapi.AlbumResponseDto{
		album("Amy's Wedding"),
		album("Amelia Birthday 2024"),
		album("Family Reunion"),
		album("Work Trip"),
		album("archive"),
	}

	tests := []struct {
		name    string
		include string
		exclude string
		want    []string
	}{
		{
			name: "no filters matches all, sorted by name",
			want: []string{"Amelia Birthday 2024", "Amy's Wedding", "Family Reunion", "Work Trip", "archive"},
		},
		{
			name:    "include alternation",
			include: "Amy|Amelia",
			want:    []string{"Amelia Birthday 2024", "Amy's Wedding"},
		},
		{
			name:    "include with exclude narrowing",
			include: "Amy|Amelia",
			exclude: "Birthday",
			want:    []string{"Amy's Wedding"},
		},
		{
			name:    "exclude only",
			exclude: "Amy|Amelia",
			want:    []string{"Family Reunion", "Work Trip", "archive"},
		},
		{
			name:    "no match",
			include: "^Nonexistent",
			want:    []string{},
		},
		{
			name:    "case sensitive by default",
			include: "amy",
			want:    []string{},
		},
		{
			name:    "inline case-insensitive flag",
			include: "(?i)amy",
			want:    []string{"Amy's Wedding"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := AddUsersToAlbumOptions{}
			if tc.include != "" {
				opts.Include = regexp.MustCompile(tc.include)
			}
			if tc.exclude != "" {
				opts.Exclude = regexp.MustCompile(tc.exclude)
			}

			got := albumNames(filterAlbums(all, opts))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("filterAlbums() = %v, want %v", got, tc.want)
			}
		})
	}
}

func user(name, email string) immichapi.UserResponseDto {
	return immichapi.UserResponseDto{
		Id:    openapi_types.UUID(uuid.New()),
		Name:  name,
		Email: openapi_types.Email(email),
	}
}

func userNames(users []immichapi.UserResponseDto) []string {
	out := make([]string, 0, len(users))
	for _, u := range users {
		out = append(out, u.Name)
	}
	return out
}

func TestMatchUsers(t *testing.T) {
	all := []immichapi.UserResponseDto{
		user("Julia Roberts", "julia@example.com"),
		user("Julian Smith", "julian@example.com"),
		user("Amy Adams", "amy@example.com"),
		user("Bob Jones", "bob@julia.dev"),
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "exact name match still requires substring semantics",
			query: "Amy Adams",
			want:  []string{"Amy Adams"},
		},
		{
			name:  "case-insensitive substring matches on both name and email",
			query: "julia",
			want:  []string{"Julia Roberts", "Julian Smith", "Bob Jones"},
		},
		{
			name:  "substring on email domain",
			query: "julia.dev",
			want:  []string{"Bob Jones"},
		},
		{
			name:  "no match",
			query: "nonexistent",
			want:  []string{},
		},
		{
			name:  "uppercase query matches lowercase data",
			query: "AMY",
			want:  []string{"Amy Adams"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := userNames(matchUsers(all, tc.query))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("matchUsers(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestAlbumHasUser(t *testing.T) {
	target := user("Julia Roberts", "julia@example.com")
	other := user("Bob Jones", "bob@example.com")

	a := immichapi.AlbumResponseDto{
		AlbumUsers: []immichapi.AlbumUserResponseDto{
			{User: other, Role: immichapi.AlbumUserRoleEditor},
			{User: target, Role: immichapi.AlbumUserRoleViewer},
		},
	}

	role, ok := AlbumHasUser(a, target.Id)
	if !ok {
		t.Fatalf("expected album to already have user %s", target.Id)
	}
	if role != immichapi.AlbumUserRoleViewer {
		t.Errorf("role = %v, want %v", role, immichapi.AlbumUserRoleViewer)
	}

	unknown := user("Nobody", "nobody@example.com")
	if _, ok := AlbumHasUser(a, unknown.Id); ok {
		t.Errorf("expected album to NOT have user %s", unknown.Id)
	}
}
