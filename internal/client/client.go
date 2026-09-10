// Package client wraps the generated Immich API client with authentication
// and base-URL handling. All API access goes through this package; application
// code must not construct immichapi clients directly.
package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dhcgn/immich-admin-cli/internal/config"
	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

// Client is a thin wrapper around the generated OpenAPI client.
// Future cross-cutting concerns (error mapping, pagination helpers,
// rate limiting) belong here.
type Client struct {
	// API exposes the generated typed operations (e.g. GetMyUserWithResponse).
	API *immichapi.ClientWithResponses
	// Server is the origin of the Immich instance (e.g. https://immich.example.com/).
	Server string
}

// New builds an authenticated client from the config. The spec declares
// servers: [{"url": "/api"}], so the base URL is the server origin + /api.
func New(cfg *config.Config) (*Client, error) {
	baseURL := strings.TrimRight(cfg.Server, "/") + "/api"
	apiKey := cfg.APIKey

	api, err := immichapi.NewClientWithResponses(baseURL,
		immichapi.WithHTTPClient(httpClient()),
		immichapi.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("x-api-key", apiKey)
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("creating API client for %q: %w", baseURL, err)
	}
	return &Client{API: api, Server: strings.TrimRight(cfg.Server, "/")}, nil
}

// httpClient returns an http.Client that fails fast on unreachable servers
// but has NO overall request timeout: large asset downloads legitimately run
// for minutes, so limits are placed on connection setup and response-header
// arrival instead of total request duration.
func httpClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: transport}
}

// Response is the subset of methods every generated *WithResponse type
// implements, allowing one status check to serve all operations.
type Response interface {
	StatusCode() int
	Status() string
	GetBody() []byte
}

// Check returns a descriptive error when resp does not have the wanted
// HTTP status. Use it right after every *WithResponse call:
//
//	resp, err := c.API.GetMyUserWithResponse(ctx)
//	if err != nil { ... }
//	if err := client.Check(resp, http.StatusOK); err != nil { return err }
func Check(resp Response, want int) error {
	if resp.StatusCode() != want {
		return fmt.Errorf("server returned %s (expected %d): %s",
			resp.Status(), want, strings.TrimSpace(string(resp.GetBody())))
	}
	return nil
}

// MinServerVersion is the oldest Immich server this CLI supports (3.2.0:
// cursor search pagination, workflow logs). Compared numerically against
// GET /server/version; the prerelease number is ignored.
const (
	MinServerMajor = 3
	MinServerMinor = 2
	MinServerPatch = 0
)

// MinServerVersionString renders the minimum supported server version for
// user-facing messages (e.g. "3.2.0").
func MinServerVersionString() string {
	return fmt.Sprintf("%d.%d.%d", MinServerMajor, MinServerMinor, MinServerPatch)
}

// ServerVersion fetches the server version (GET /server/version,
// getServerVersion).
func (c *Client) ServerVersion(ctx context.Context) (immichapi.ServerVersionResponseDto, error) {
	resp, err := c.API.GetServerVersionWithResponse(ctx)
	if err != nil {
		return immichapi.ServerVersionResponseDto{}, fmt.Errorf("calling GET /server/version: %w", err)
	}
	if err := Check(resp, http.StatusOK); err != nil {
		return immichapi.ServerVersionResponseDto{}, fmt.Errorf("GET /server/version: %w", err)
	}
	return *resp.JSON200, nil
}

// ServerTooOld reports whether v is below the minimum supported server
// version (numeric major/minor/patch comparison; prerelease ignored). Pure
// so the version gate is directly unit-testable.
func ServerTooOld(v immichapi.ServerVersionResponseDto) bool {
	if v.Major != MinServerMajor {
		return v.Major < MinServerMajor
	}
	if v.Minor != MinServerMinor {
		return v.Minor < MinServerMinor
	}
	return v.Patch < MinServerPatch
}

// FormatServerVersion renders a version DTO as "3.2.0" (with "-rc.N" suffix
// when the server reports a prerelease number).
func FormatServerVersion(v immichapi.ServerVersionResponseDto) string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease != nil {
		s += fmt.Sprintf("-rc.%d", *v.Prerelease)
	}
	return s
}

// CheckServerVersion fetches the server version and returns a descriptive
// error when it is below the minimum supported version. Callers decide
// whether that error aborts (fail fast) or just warns; every CLI command
// warns via newClient so the requirement stays transparent.
func (c *Client) CheckServerVersion(ctx context.Context) error {
	v, err := c.ServerVersion(ctx)
	if err != nil {
		return err
	}
	if ServerTooOld(v) {
		return fmt.Errorf("server is %s, this CLI needs Immich >= %s; some commands may misbehave",
			FormatServerVersion(v), MinServerVersionString())
	}
	return nil
}

// PrintIdentity fetches the current user and prints the identity line
// (email + server URL + server version) to stderr so users always know
// which account and instance — and which minimum version applies — they
// are operating against. A version-fetch failure degrades to "unknown"
// rather than hiding the identity line.
func (c *Client) PrintIdentity(ctx context.Context) {
	resp, err := c.API.GetMyUserWithResponse(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ could not verify identity: %v\n", err)
		return
	}
	if resp.StatusCode() != http.StatusOK {
		fmt.Fprintf(os.Stderr, "⚠ could not verify identity: %s\n", resp.Status())
		return
	}
	version := "unknown"
	if v, err := c.ServerVersion(ctx); err == nil {
		version = FormatServerVersion(v)
	}
	fmt.Fprintf(os.Stderr, "%s (%s, server %s, needs >= %s)\n",
		resp.JSON200.Email, c.Server, version, MinServerVersionString())
}
