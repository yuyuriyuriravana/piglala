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
