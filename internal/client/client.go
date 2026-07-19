// Package client wraps the generated Immich API client with authentication
// and base-URL handling. All API access goes through this package; application
// code must not construct immichapi clients directly.
package client

import (
	"context"
	"fmt"
	"net/http"
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
	return &Client{API: api}, nil
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
