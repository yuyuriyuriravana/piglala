package main

import "testing"

func TestOrderedBestParsesShowsLatestTierAndUltimates(t *testing.T) {
	bests := map[int]BestParse{
		78:  {EncounterName: "Erichthonios", RankPercent: 99.1},
		101: {EncounterName: "Dancing Green", RankPercent: 91.2},
		501: {EncounterName: "Extreme Trial", RankPercent: 88.8},
		601: {EncounterName: "The Omega Protocol", RankPercent: 80.5},
	}

	got := orderedBestParses(bests)
	if len(got) != 2 {
		t.Fatalf("orderedBestParses() returned %d entries, want latest tier plus ultimate", len(got))
	}
	if got[0].encounterID != 101 {
		t.Fatalf("first encounter = %d, want latest Savage encounter 101", got[0].encounterID)
	}
	if got[1].encounterID != 601 {
		t.Fatalf("second encounter = %d, want Ultimate encounter 601", got[1].encounterID)
	}
	if got[1].order.tierName != ultimatesTierName {
		t.Fatalf("ultimate encounter tier = %q, want %q", got[1].order.tierName, ultimatesTierName)
	}
}

func TestOrderedBestParsesIgnoresStaleUltimateEncounterIDs(t *testing.T) {
	bests := map[int]BestParse{
		1060: {EncounterName: "Zoraal Ja", RankPercent: 48.3},
		1061: {EncounterName: "The Weapon's Refrain", RankPercent: 93.6},
	}

	got := orderedBestParses(bests)
	if len(got) != 1 {
		t.Fatalf("orderedBestParses() returned %d entries, want only real Ultimate", len(got))
	}
	if got[0].best.EncounterName != "The Weapon's Refrain" {
		t.Fatalf("encounter = %q, want The Weapon's Refrain", got[0].best.EncounterName)
	}
}

func TestOrderedBestParsesPrefersCanonicalUltimateEncounterID(t *testing.T) {
	bests := map[int]BestParse{
		1060: {EncounterName: "The Unending Coil of Bahamut", RankPercent: 100.0},
		1073: {EncounterName: "The Unending Coil of Bahamut", RankPercent: 70.0},
	}

	got := orderedBestParses(bests)
	if len(got) != 1 {
		t.Fatalf("orderedBestParses() returned %d entries, want one deduplicated Ultimate", len(got))
	}
	if got[0].encounterID != 1073 || got[0].best.RankPercent != 70.0 {
		t.Fatalf("deduplicated encounter = id %d pct %.1f, want id 1073 pct 70.0", got[0].encounterID, got[0].best.RankPercent)
	}
}

func TestIsUltimateEncounter(t *testing.T) {
	tests := []struct {
		name          string
		encounterName string
		want          bool
	}{
		{name: "known name", encounterName: "Futures Rewritten", want: true},
		{name: "explicit suffix", encounterName: "Some Future Fight (Ultimate)", want: true},
		{name: "extreme excluded", encounterName: "Extreme Trial", want: false},
		{name: "savage excluded", encounterName: "Dancing Green", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUltimateEncounter(tt.encounterName); got != tt.want {
				t.Fatalf("isUltimateEncounter(%q) = %t, want %t", tt.encounterName, got, tt.want)
			}
		})
	}
}
