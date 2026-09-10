package client

import (
	"testing"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func ver(major, minor, patch int) immichapi.ServerVersionResponseDto {
	return immichapi.ServerVersionResponseDto{Major: major, Minor: minor, Patch: patch}
}

func TestServerTooOld(t *testing.T) {
	tests := []struct {
		v    immichapi.ServerVersionResponseDto
		want bool
	}{
		{ver(2, 9, 9), true},
		{ver(3, 1, 9), true},
		{ver(3, 2, 0), false},
		{ver(3, 2, 1), false},
		{ver(3, 3, 0), false},
		{ver(4, 0, 0), false},
	}
	for _, tc := range tests {
		if got := ServerTooOld(tc.v); got != tc.want {
			t.Errorf("ServerTooOld(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}

	// Prerelease is ignored: 3.2.0-rc.0 satisfies the 3.2.0 floor.
	pre := 0
	if ServerTooOld(immichapi.ServerVersionResponseDto{Major: 3, Minor: 2, Patch: 0, Prerelease: &pre}) {
		t.Error("ServerTooOld(3.2.0-rc.0) = true, want false (prerelease ignored)")
	}
}

func TestFormatServerVersion(t *testing.T) {
	if got := FormatServerVersion(ver(3, 2, 0)); got != "3.2.0" {
		t.Errorf("FormatServerVersion(3.2.0) = %q, want %q", got, "3.2.0")
	}
	pre := 0
	got := FormatServerVersion(immichapi.ServerVersionResponseDto{Major: 3, Minor: 2, Patch: 0, Prerelease: &pre})
	if got != "3.2.0-rc.0" {
		t.Errorf("FormatServerVersion(3.2.0-rc.0) = %q, want %q", got, "3.2.0-rc.0")
	}
	if MinServerVersionString() != "3.2.0" {
		t.Errorf("MinServerVersionString() = %q, want %q", MinServerVersionString(), "3.2.0")
	}
}
