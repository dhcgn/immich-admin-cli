// Package clipprobe talks to the external immich-clip-probe service
// (https://github.com/dhcgn/immich-clip-probe/), which exposes Immich's
// CLIP similarity search for files that are not in Immich yet.
// It is the only place that does raw HTTP for clip-probe; command and
// workflow code must go through Client.
package clipprobe

// Query describes what the service actually embedded.
type Query struct {
	FileName   string `json:"fileName,omitempty"`
	Bytes      int64  `json:"bytes"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Normalized bool   `json:"normalized"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
}

// Links holds deep links to a match. Absent unless the service is
// configured with PUBLIC_IMMICH_URL.
type Links struct {
	Web       string `json:"web,omitempty"`
	Thumbnail string `json:"thumbnail,omitempty"`
}

// Match is one visually-nearest asset, ascending by Distance.
type Match struct {
	AssetID          string  `json:"assetId"`
	OriginalFileName string  `json:"originalFileName"`
	Type             string  `json:"type"`
	LocalDateTime    string  `json:"localDateTime,omitempty"`
	FileCreatedAt    string  `json:"fileCreatedAt,omitempty"`
	OwnerID          string  `json:"ownerId,omitempty"`
	Distance         float64 `json:"distance"`
	Similarity       float64 `json:"similarity"`
	Links            *Links  `json:"links,omitempty"`
}

// Timings holds per-stage wall-clock milliseconds.
type Timings struct {
	NormalizeMs int64 `json:"normalizeMs,omitempty"`
	EmbedMs     int64 `json:"embedMs,omitempty"`
	QueryMs     int64 `json:"queryMs,omitempty"`
	TotalMs     int64 `json:"totalMs,omitempty"`
}

// SimilarResponse is the POST /v1/similar payload. An empty Matches array
// is a normal result (nothing close enough).
type SimilarResponse struct {
	Query       Query    `json:"query"`
	Duplicate   bool     `json:"duplicate"`
	MaxDistance float64  `json:"maxDistance"`
	Matches     []Match  `json:"matches"`
	Timings     *Timings `json:"timings,omitempty"`
}

// ErrorBody is the service's error shape. Match on Error, never on Message.
type ErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
