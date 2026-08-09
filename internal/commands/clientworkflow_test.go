package commands

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func testAlbum(name string) immichapi.AlbumResponseDto {
	return immichapi.AlbumResponseDto{Id: openapi_types.UUID(uuid.New()), AlbumName: name, AssetCount: 1}
}

func albumNames(albums []immichapi.AlbumResponseDto) []string {
	out := make([]string, 0, len(albums))
	for _, a := range albums {
		out = append(out, a.AlbumName)
	}
	return out
}

func TestSelectAlbumsInteractively(t *testing.T) {
	user := immichapi.UserResponseDto{Id: openapi_types.UUID(uuid.New()), Name: "Julia", Email: "julia@example.com"}

	alreadyShared := testAlbum("Already Shared")
	alreadyShared.AlbumUsers = []immichapi.AlbumUserResponseDto{
		{User: user, Role: immichapi.AlbumUserRoleViewer},
	}

	albums := []immichapi.AlbumResponseDto{
		testAlbum("Amy's Wedding"),
		testAlbum("Amelia Birthday"),
		alreadyShared,
		testAlbum("Work Trip"),
	}

	tests := []struct {
		name   string
		answer string // one line per prompted (non-already-shared) album
		want   []string
	}{
		{
			name:   "all yes",
			answer: "y\ny\ny\n",
			want:   []string{"Amy's Wedding", "Amelia Birthday", "Already Shared", "Work Trip"},
		},
		{
			name:   "all no",
			answer: "n\nn\nn\n",
			want:   []string{"Already Shared"},
		},
		{
			name:   "mixed answers",
			answer: "y\nn\nyes\n",
			want:   []string{"Amy's Wedding", "Already Shared", "Work Trip"},
		},
		{
			name:   "empty line treated as no",
			answer: "\n\n\n",
			want:   []string{"Already Shared"},
		},
		{
			name:   "EOF stops asking and skips all remaining albums",
			answer: "y\n",
			want:   []string{"Amy's Wedding"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.NewReader(tc.answer)
			var out bytes.Buffer

			got := selectAlbumsInteractively(in, &out, albums, user, immichapi.AlbumUserRoleViewer)
			if !reflect.DeepEqual(albumNames(got), tc.want) {
				t.Errorf("selectAlbumsInteractively() = %v, want %v", albumNames(got), tc.want)
			}

			// Already-shared albums must never be prompted for.
			if strings.Contains(out.String(), "Already Shared") {
				t.Errorf("prompted for already-shared album, output: %q", out.String())
			}
		})
	}
}

func TestSelectAlbumsInteractivelyNoAlbums(t *testing.T) {
	user := immichapi.UserResponseDto{Id: openapi_types.UUID(uuid.New()), Name: "Julia"}
	var out bytes.Buffer

	got := selectAlbumsInteractively(strings.NewReader(""), &out, nil, user, immichapi.AlbumUserRoleViewer)
	if len(got) != 0 {
		t.Errorf("expected no albums selected, got %v", got)
	}
	if out.Len() != 0 {
		t.Errorf("expected no prompts printed, got %q", out.String())
	}
}
