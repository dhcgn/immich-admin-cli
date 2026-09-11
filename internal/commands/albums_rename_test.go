package commands

import "testing"

func TestValidateAlbumRenameFlags(t *testing.T) {
	tests := []struct {
		name      string
		newName   string
		albumID   string
		albumName string
		wantErr   bool
	}{
		{name: "id target", newName: "New", albumID: "some-id", wantErr: false},
		{name: "name target", newName: "New", albumName: "Old", wantErr: false},
		{name: "empty name", newName: "", albumID: "some-id", wantErr: true},
		{name: "blank name", newName: "   ", albumName: "Old", wantErr: true},
		{name: "neither target", newName: "New", wantErr: true},
		{name: "both targets", newName: "New", albumID: "some-id", albumName: "Old", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAlbumRenameFlags(tc.newName, tc.albumID, tc.albumName)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateAlbumRenameFlags(%q, %q, %q) error = %v, wantErr %v", tc.newName, tc.albumID, tc.albumName, err, tc.wantErr)
			}
		})
	}
}
