package workflows

import (
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func TestRenderTagPattern(t *testing.T) {
	got, err := RenderTagPattern("immich-admin-cli/watch/{yyyy-MM-dd}", time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC))
	if err != nil || got != "immich-admin-cli/watch/2026-09-19" {
		t.Fatalf("RenderTagPattern = %q, %v; want watch/2026-09-19", got, err)
	}
	if _, err := RenderTagPattern("x/{yyyy-MM}", time.Now()); err == nil {
		t.Error("unknown placeholder expected error, got nil")
	}
}

func TestClassifyBulkCheckResult(t *testing.T) {
	dup := immichapi.AssetRejectReasonDuplicate
	id := openapi_types.UUID([16]byte{1})
	tr := true
	got := ClassifyBulkCheckResult("f", "c", immichapi.AssetBulkUploadCheckResult{Action: immichapi.Reject, Reason: &dup, AssetId: &id, IsTrashed: &tr, Id: "f"})
	if got.Status != BulkCheckUploaded || got.AssetID == nil || !got.IsTrashed {
		t.Fatalf("duplicate classify = %+v, want uploaded with id+trashed", got)
	}
	got = ClassifyBulkCheckResult("f", "c", immichapi.AssetBulkUploadCheckResult{Action: immichapi.Accept, Id: "f"})
	if got.Status != BulkCheckMissing {
		t.Fatalf("accept classify = %q, want missing", got.Status)
	}
	uns := immichapi.AssetRejectReasonUnsupportedFormat
	got = ClassifyBulkCheckResult("f", "c", immichapi.AssetBulkUploadCheckResult{Action: immichapi.Reject, Reason: &uns, Id: "f"})
	if got.Status != BulkCheckUnsupported {
		t.Fatalf("unsupported classify = %q, want unsupported", got.Status)
	}
}

func TestFormatByteProgressLine(t *testing.T) {
	line := FormatByteProgressLine(3, 42, "IMG.jpg", 45, 100, time.Second)
	if line == "" || len(line) < 10 {
		t.Fatalf("empty progress line: %q", line)
	}
	// Unknown total: bytes+rate only, no %.
	line = FormatByteProgressLine(1, 2, "f", 10, -1, time.Second)
	for _, s := range []string{"[1/2]", "f"} {
		found := false
		for i := 0; i+len(s) <= len(line); i++ {
			if line[i:i+len(s)] == s {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("progress line %q missing %q", line, s)
		}
	}
}

func TestBulkIDFailure(t *testing.T) {
	id := openapi_types.UUID([16]byte{2})
	if got := bulkIDFailure(immichapi.BulkIdResponseDto{Id: id, Success: true}); got != "" {
		t.Fatalf("success = %q, want empty", got)
	}
	dup := immichapi.BulkIdErrorReasonDuplicate
	if got := bulkIDFailure(immichapi.BulkIdResponseDto{Id: id, Error: &dup}); got != "" {
		t.Fatalf("duplicate = %q, want empty (already in album)", got)
	}
	perm := immichapi.BulkIdErrorReasonNoPermission
	msg := "denied"
	if got := bulkIDFailure(immichapi.BulkIdResponseDto{Id: id, Error: &perm, ErrorMessage: &msg}); got != "denied" {
		t.Fatalf("no_permission = %q, want %q", got, msg)
	}
	if got := bulkIDFailure(immichapi.BulkIdResponseDto{Id: id, Error: &perm}); got != "no_permission" {
		t.Fatalf("no_permission bare = %q, want the reason value, not a pointer", got)
	}
}

func TestStableForGate(t *testing.T) {
	st := map[string]watchUploadEntry{}
	now := time.Now()
	mtime := now.Add(-time.Minute)
	if stableForGate(st, "a.jpg", 10, mtime, now, 30*time.Second) {
		t.Fatal("first sighting should be unstable")
	}
	// Same size+mtime 31s later -> stable.
	if !stableForGate(st, "a.jpg", 10, mtime, now.Add(31*time.Second), 30*time.Second) {
		t.Fatal("unchanged after stable-for should be stable")
	}
	// Growing file resets.
	if stableForGate(st, "a.jpg", 11, now, now.Add(32*time.Second), 30*time.Second) {
		t.Fatal("changed size should be unstable")
	}
}
