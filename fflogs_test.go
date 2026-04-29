package main

import "testing"

func TestIsRelevantDifficultyTracksHighEndFights(t *testing.T) {
	tests := []struct {
		name       string
		difficulty string
		want       bool
	}{
		{name: "savage", difficulty: "Savage", want: true},
		{name: "extreme", difficulty: "Extreme", want: true},
		{name: "ultimate", difficulty: "Ultimate", want: true},
		{name: "case and whitespace", difficulty: " extreme ", want: true},
		{name: "normal excluded", difficulty: "Normal", want: false},
		{name: "empty excluded", difficulty: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRelevantDifficulty(tt.difficulty); got != tt.want {
				t.Fatalf("isRelevantDifficulty(%q) = %t, want %t", tt.difficulty, got, tt.want)
			}
		})
	}
}

func TestRelevantContentTypeTracksHighEndZones(t *testing.T) {
	tests := []struct {
		name       string
		zone       string
		difficulty string
		want       string
	}{
		{name: "savage difficulty", zone: "AAC Heavyweight", difficulty: "Savage", want: "Savage"},
		{name: "extreme difficulty", zone: "Some Zone", difficulty: "Extreme", want: "Extreme"},
		{name: "ultimate difficulty", zone: "Some Zone", difficulty: "Ultimate", want: "Ultimate"},
		{name: "extreme trial zone", zone: "Dawntrail Trials", difficulty: "Normal", want: "Extreme"},
		{name: "ultimate zone", zone: "The Weapon's Refrain", difficulty: "Normal", want: "Ultimate"},
		{name: "untracked normal", zone: "Dungeons", difficulty: "Normal", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := relevantContentType(tt.zone, tt.difficulty); got != tt.want {
				t.Fatalf("relevantContentType(%q, %q) = %q, want %q", tt.zone, tt.difficulty, got, tt.want)
			}
		})
	}
}
