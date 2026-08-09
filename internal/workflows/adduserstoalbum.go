package workflows

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/client"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// AddUsersToAlbumOptions controls the add-users-to-album workflow.
type AddUsersToAlbumOptions struct {
	// Include, when non-nil, keeps only albums whose AlbumName matches it.
	// A nil Include matches every album.
	Include *regexp.Regexp
	// Exclude, when non-nil, drops any album whose AlbumName matches it,
	// even if it matched Include. A nil Exclude excludes nothing.
	Exclude *regexp.Regexp
	// Role is the album role granted to the target user (e.g. "viewer").
	Role immichapi.AlbumUserRole
	// DryRun prints the planned share steps without calling the API.
	DryRun bool
}

// SelectAlbumsForSharing fetches all albums (GET /albums) and returns those
// whose AlbumName matches opts.Include and does not match opts.Exclude,
// sorted by AlbumName for deterministic output.
//
// It performs no sharing — this is the read-only "getting the information"
// path used both by the command's review step and by the integration test.
func SelectAlbumsForSharing(ctx context.Context, c *client.Client, opts AddUsersToAlbumOptions) ([]immichapi.AlbumResponseDto, error) {
	resp, err := c.API.GetAllAlbumsWithResponse(ctx, &immichapi.GetAllAlbumsParams{})
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching albums: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("fetching albums: response had no body")
	}

	return filterAlbums(*resp.JSON200, opts), nil
}

// filterAlbums returns the albums whose AlbumName matches opts.Include and
// does not match opts.Exclude, sorted by AlbumName. It is pure (no network)
// so the selection logic can be unit-tested in isolation.
func filterAlbums(albums []immichapi.AlbumResponseDto, opts AddUsersToAlbumOptions) []immichapi.AlbumResponseDto {
	var matched []immichapi.AlbumResponseDto
	for _, album := range albums {
		if opts.Include != nil && !opts.Include.MatchString(album.AlbumName) {
			continue
		}
		if opts.Exclude != nil && opts.Exclude.MatchString(album.AlbumName) {
			continue
		}
		matched = append(matched, album)
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].AlbumName < matched[j].AlbumName
	})
	return matched
}

// ResolveUser identifies exactly one user matching query: a raw UUID is
// matched by exact ID; anything else is resolved by fetching all users
// (GET /users) and applying matchUsers (case-insensitive substring match on
// name or email). It returns an error listing all candidates if there are
// zero or more than one match.
func ResolveUser(ctx context.Context, c *client.Client, query string) (*immichapi.UserResponseDto, error) {
	if id, err := uuid.Parse(query); err == nil {
		resp, err := c.API.GetUserWithResponse(ctx, openapi_types.UUID(id))
		if err == nil {
			err = client.Check(resp, http.StatusOK)
		}
		if err != nil {
			return nil, fmt.Errorf("fetching user %s: %w", query, err)
		}
		return resp.JSON200, nil
	}

	resp, err := c.API.SearchUsersWithResponse(ctx)
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching users: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("fetching users: response had no body")
	}

	matches := matchUsers(*resp.JSON200, query)
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no user matches %q; pass an exact email, name substring, or user UUID", query)
	case 1:
		return &matches[0], nil
	default:
		names := make([]string, 0, len(matches))
		for _, u := range matches {
			names = append(names, fmt.Sprintf("%s <%s> (%s)", u.Name, u.Email, u.Id))
		}
		return nil, fmt.Errorf("query %q is ambiguous, matches %d users: %s; narrow it down (e.g. use the exact email or UUID)",
			query, len(matches), strings.Join(names, ", "))
	}
}

// matchUsers returns the users whose Name or Email contains query
// (case-insensitive substring match). It is pure (no network) so the
// resolution logic can be unit-tested in isolation.
func matchUsers(users []immichapi.UserResponseDto, query string) []immichapi.UserResponseDto {
	q := strings.ToLower(query)
	var matched []immichapi.UserResponseDto
	for _, u := range users {
		if strings.Contains(strings.ToLower(u.Name), q) || strings.Contains(strings.ToLower(string(u.Email)), q) {
			matched = append(matched, u)
		}
	}
	return matched
}

// AlbumHasUser reports whether album already lists userID among its members,
// returning that member's role if so. Exported so callers (e.g. the
// review/listing step before sharing) can show existing membership without
// duplicating this lookup.
func AlbumHasUser(album immichapi.AlbumResponseDto, userID openapi_types.UUID) (immichapi.AlbumUserRole, bool) {
	for _, au := range album.AlbumUsers {
		if au.User.Id == userID {
			return au.Role, true
		}
	}
	return "", false
}

// ShareAlbumsWithUser shares each album with user at the configured role
// (PUT /albums/{id}/users), continuing on error and returning a summary
// error if any share failed (the same bulk convention used by the other
// workflows). Albums where the user already has access are skipped with an
// informational message and are not counted as failures. In dry-run mode it
// prints the planned step for each album (or the "already shared" notice)
// and shares nothing.
func ShareAlbumsWithUser(ctx context.Context, c *client.Client, albums []immichapi.AlbumResponseDto, user immichapi.UserResponseDto, opts AddUsersToAlbumOptions) error {
	return RunBatch(albums,
		func(a immichapi.AlbumResponseDto) string { return a.AlbumName },
		func(a immichapi.AlbumResponseDto) error {
			if role, already := AlbumHasUser(a, user.Id); already {
				if opts.DryRun {
					fmt.Printf("[dry-run] %s: %s already has access (role %s), would skip\n", a.AlbumName, user.Name, role)
				} else {
					fmt.Printf("%s: %s already has access (role %s), skipping\n", a.AlbumName, user.Name, role)
				}
				return nil
			}

			steps := []Step{
				{
					Name: fmt.Sprintf("Share album %q (%s) with %s <%s> as %s", a.AlbumName, a.Id, user.Name, user.Email, opts.Role),
					Run: func(ctx context.Context) error {
						return addUserToAlbum(ctx, c, a.Id, user.Id, opts.Role)
					},
				},
			}
			return RunSteps(ctx, RunOptions{DryRun: opts.DryRun}, a.AlbumName, steps)
		},
	)
}

// addUserToAlbum shares a single album with a single user at the given role
// (PUT /albums/{id}/users).
func addUserToAlbum(ctx context.Context, c *client.Client, albumID, userID openapi_types.UUID, role immichapi.AlbumUserRole) error {
	body := immichapi.AddUsersToAlbumJSONRequestBody{
		AlbumUsers: []immichapi.AlbumUserAddDto{
			{UserId: userID, Role: &role},
		},
	}
	resp, err := c.API.AddUsersToAlbumWithResponse(ctx, albumID, body)
	if err == nil {
		err = client.Check(resp, http.StatusOK)
	}
	if err != nil {
		return fmt.Errorf("sharing album: %w", err)
	}
	return nil
}
