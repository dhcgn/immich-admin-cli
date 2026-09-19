package commands

import (
	"testing"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func bulkResult(success bool, reason immichapi.BulkIdErrorReason) immichapi.BulkIdResponseDto {
	r := immichapi.BulkIdResponseDto{Id: openapi_types.UUID(uuid.New()), Success: success}
	if !success {
		r.Error = &reason
	}
	return r
}

func TestTallyBulkResults(t *testing.T) {
	perm := immichapi.BulkIdErrorReasonNoPermission
	results := []immichapi.BulkIdResponseDto{
		{Id: openapi_types.UUID(uuid.New()), Success: true},
		{Id: openapi_types.UUID(uuid.New()), Success: true},
		bulkResult(false, immichapi.BulkIdErrorReasonDuplicate),
		bulkResult(false, immichapi.BulkIdErrorReasonNotFound),
		{Id: openapi_types.UUID(uuid.New()), Success: false, Error: &perm},
		{Id: openapi_types.UUID(uuid.New()), Success: false}, // no reason at all
	}

	got := tallyBulkResults(results)
	if got.succeeded != 2 || got.alreadyPresent != 1 || got.notFound != 1 || len(got.failures) != 2 {
		t.Errorf("tallyBulkResults = %+v, want {succeeded:2 alreadyPresent:1 notFound:1 failures:2}", got)
	}

	if got := tallyBulkResults(nil); got.succeeded != 0 || len(got.failures) != 0 {
		t.Errorf("tallyBulkResults(nil) = %+v, want zeros", got)
	}
}

func TestReportAlbumBulkResults(t *testing.T) {
	ok := []immichapi.BulkIdResponseDto{{Id: openapi_types.UUID(uuid.New()), Success: true}}
	if err := reportAlbumBulkResults("added", ok); err != nil {
		t.Errorf("reportAlbumBulkResults(all ok) error = %v, want nil", err)
	}
	bad := []immichapi.BulkIdResponseDto{{Id: openapi_types.UUID(uuid.New()), Success: false}}
	if err := reportAlbumBulkResults("added", bad); err == nil {
		t.Error("reportAlbumBulkResults(failure) expected error, got nil")
	}
}

func TestValidateMergeAlbumFlags(t *testing.T) {
	a := uuid.New().String()
	b := uuid.New().String()
	if _, _, err := validateMergeAlbumFlags(a, b); err != nil {
		t.Errorf("validateMergeAlbumFlags(%q, %q) error = %v, want nil", a, b, err)
	}
	if _, _, err := validateMergeAlbumFlags(a, a); err == nil {
		t.Error("validateMergeAlbumFlags(same, same) expected error, got nil")
	}
	if _, _, err := validateMergeAlbumFlags("not-a-uuid", b); err == nil {
		t.Error("validateMergeAlbumFlags(bad from) expected error, got nil")
	}
	if _, _, err := validateMergeAlbumFlags(a, ""); err == nil {
		t.Error("validateMergeAlbumFlags(empty into) expected error, got nil")
	}
}
