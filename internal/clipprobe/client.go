package clipprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ValidTypes is the closed set accepted by POST /v1/similar ?type=.
var ValidTypes = []string{"IMAGE", "VIDEO", "all"}

// Options maps 1:1 to the /v1/similar query parameters.
type Options struct {
	Limit       int
	MaxDistance float64
	All         bool
	Type        string
}

// DefaultOptions mirrors the service defaults (openapi.yaml).
func DefaultOptions() Options {
	return Options{Limit: 10, MaxDistance: 0.01, Type: "IMAGE"}
}

// Validate rejects values outside the service's closed set before any I/O.
func (o Options) Validate() error {
	if o.Limit < 1 || o.Limit > 100 {
		return fmt.Errorf("invalid --limit %d: must be between 1 and 100", o.Limit)
	}
	if o.MaxDistance < 0 || o.MaxDistance > 2 {
		return fmt.Errorf("invalid --max-distance %v: must be between 0 and 2", o.MaxDistance)
	}
	if slices.Contains(ValidTypes, o.Type) {
		return nil
	}
	return fmt.Errorf("invalid --type %q: must be one of %s", o.Type, strings.Join(ValidTypes, ", "))
}

// Client talks to one clip-probe service. No Immich credentials are ever
// sent here — the token is the service's own API_TOKEN (x-api-key header).
type Client struct {
	server string
	token  string
	http   *http.Client
}

// New builds a Client for the service origin (e.g. https://clip-probe.example.com/).
func New(server, token string) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &Client{server: strings.TrimRight(server, "/"), token: token, http: &http.Client{Transport: transport}}
}

// FindSimilar posts one image file and returns the decoded response plus the
// raw body (for --json output). An empty Matches is not an error.
func (c *Client) FindSimilar(ctx context.Context, filePath string, opts Options) (*SimilarResponse, []byte, error) {
	if err := opts.Validate(); err != nil {
		return nil, nil, err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	q := url.Values{}
	q.Set("limit", strconv.Itoa(opts.Limit))
	q.Set("max_distance", strconv.FormatFloat(opts.MaxDistance, 'f', -1, 64))
	if opts.All {
		q.Set("all", "true")
	}
	if opts.Type != "" {
		q.Set("type", opts.Type)
	}
	endpoint := c.server + "/v1/similar?" + q.Encode()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(filepath.Base(filePath))))
	h.Set("Content-Type", "application/octet-stream")
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, nil, fmt.Errorf("building multipart body: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, nil, fmt.Errorf("reading file: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, nil, fmt.Errorf("building multipart body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("x-api-key", c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("calling POST /v1/similar: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var eb ErrorBody
		if json.Unmarshal(raw, &eb) == nil && eb.Error != "" {
			return nil, nil, fmt.Errorf("clip-probe returned %s (%s): %s", resp.Status, eb.Error, eb.Message)
		}
		return nil, nil, fmt.Errorf("clip-probe returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var out SimilarResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, fmt.Errorf("decoding response: %w", err)
	}
	return &out, raw, nil
}

// escapeQuotes keeps a hostile file name inside the multipart header.
func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, "_")
}
