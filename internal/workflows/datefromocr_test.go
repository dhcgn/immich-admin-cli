package workflows

import (
	"slices"
	"testing"
	"time"

	"github.com/dhcgn/immich-admin-cli/internal/immichapi"
)

func box(text string, score float64) immichapi.AssetOcrResponseDto {
	return immichapi.AssetOcrResponseDto{Text: text, TextScore: score}
}

func TestExtractOCRYears(t *testing.T) {
	tests := []struct {
		name  string
		boxes []immichapi.AssetOcrResponseDto
		years []int
		texts []string
	}{
		// The two real prod samples the feature was designed against.
		{"year inside text", []immichapi.AssetOcrResponseDto{box("Agypten 2004", 0.94)}, []int{2004}, []string{"Agypten 2004"}},
		{"bare year", []immichapi.AssetOcrResponseDto{box("1986", 1.0)}, []int{1986}, []string{"1986"}},
		{"date format", []immichapi.AssetOcrResponseDto{box("14.07.1986", 0.9)}, []int{1986}, []string{"14.07.1986"}},
		{"low score ignored", []immichapi.AssetOcrResponseDto{box("1986", 0.5)}, nil, nil},
		{"future year ignored", []immichapi.AssetOcrResponseDto{box("2099", 1.0)}, nil, nil},
		{"longer number ignored", []immichapi.AssetOcrResponseDto{box("Tel 019860", 1.0), box("1700", 1.0)}, nil, nil},
		{"distinct sorted", []immichapi.AssetOcrResponseDto{box("2004", 1.0), box("1986 und 2004", 1.0)}, []int{1986, 2004}, []string{"2004", "1986 und 2004"}},
		{"duplicate boxes shown once", []immichapi.AssetOcrResponseDto{box("1986", 1.0), box("1986", 0.9)}, []int{1986}, []string{"1986"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			years, texts := ExtractOCRYears(tt.boxes, DefaultOCRMinScore, 2026)
			if !slices.Equal(years, tt.years) || !slices.Equal(texts, tt.texts) {
				t.Errorf("got years %v texts %q, want %v %q", years, texts, tt.years, tt.texts)
			}
		})
	}
}

func TestOCRYearCandidateStatus(t *testing.T) {
	at := func(year int) immichapi.AssetResponseDto {
		return immichapi.AssetResponseDto{LocalDateTime: time.Date(year, 11, 15, 18, 46, 15, 0, time.UTC)}
	}
	tests := []struct {
		asset immichapi.AssetResponseDto
		years []int
		want  string
	}{
		{at(2009), []int{1986}, OCRStatusChange},
		{at(2004), []int{2004}, OCRStatusMatches},
		{at(2004), []int{1986, 2004}, OCRStatusAmbiguous},
	}
	for _, tt := range tests {
		if got := (OCRYearCandidate{Asset: tt.asset, Years: tt.years}).Status(); got != tt.want {
			t.Errorf("Status(asset %d, years %v) = %q, want %q", tt.asset.LocalDateTime.Year(), tt.years, got, tt.want)
		}
	}
	if got := OCRTargetDate(1986).Format("2006-01-02T15:04:05"); got != "1986-06-01T00:00:00" {
		t.Errorf("OCRTargetDate(1986) = %s, want 1986-06-01T00:00:00", got)
	}
}
