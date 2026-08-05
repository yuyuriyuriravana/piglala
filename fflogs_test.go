package main

import (
	"encoding/json"
	"testing"
	"time"
)

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

func TestDecodeRecentParseFightsReturnsAllPlayersInEachFight(t *testing.T) {
	data := []byte(`{
  "data": {"characterData": {"character": {"recentReports": {"data": [
    {
      "code": "report-a",
      "startTime": 1000000,
      "fights": [{"id": 7, "encounterID": 103, "name": "The Tyrant", "difficulty": 101, "startTime": 5000}],
      "rankings": {"data": [{
        "fightID": 7,
        "zone": 73,
        "encounter": {"id": 103, "name": "The Tyrant"},
        "difficulty": 101,
        "roles": {
          "healers": {"characters": [
            {"name": "Iyvy Ivy", "class": "Sage", "amount": 24282.643, "rankPercent": 99, "server": {"name": "Ravana", "region": "OC"}},
            {"name": "Other Player", "class": "Sage", "amount": 20000, "rankPercent": 50, "server": {"name": "Ravana", "region": "OC"}}
          ]}
        }
      }]}
    },
    {
      "code": "report-b",
      "startTime": 900000,
      "fights": [{"id": 12, "encounterID": 103, "name": "The Tyrant", "difficulty": 101, "startTime": 105000}],
      "rankings": {"data": [{
        "fightID": 12,
        "zone": 73,
        "encounter": {"id": 103, "name": "The Tyrant"},
        "difficulty": 101,
        "roles": {
          "healers": {"characters": [
            {"name": "Iyvy Ivy", "class": "Sage", "amount": 24282.643, "rankPercent": "-", "server": {"name": "ravana", "region": "oc"}}
          ]}
        }
      }]}
    }
  ]}}}},
  "errors": []
}`)

	fights, err := decodeRecentParseFights(data)
	if err != nil {
		t.Fatalf("decodeRecentParseFights: %v", err)
	}
	if len(fights) != 2 {
		t.Fatalf("fights = %d, want two copies of the same fight from separate reports", len(fights))
	}
	wantStart := time.UnixMilli(1005000).UTC()
	for _, fight := range fights {
		if fight.EncounterID != 103 || fight.EncounterName != "The Tyrant" || fight.ZoneID != 73 || fight.DifficultyID != 101 {
			t.Errorf("fight identity = %#v", fight)
		}
		if !fight.StartedAt.Equal(wantStart) {
			t.Errorf("StartedAt = %v, want %v", fight.StartedAt, wantStart)
		}
	}
	if len(fights[0].Players) != 2 {
		t.Fatalf("first fight players = %d, want 2", len(fights[0].Players))
	}
	first := fights[0].Players[0]
	if first.Player != (WatchedPlayer{Name: "Iyvy Ivy", Server: "ravana", Region: "OC"}) || first.Amount != 24282.643 || first.Job != "Sage" {
		t.Errorf("first player result = %#v", first)
	}
	if !first.HasPercent || first.RankPercent != 99 {
		t.Errorf("first percentile = %#v, want 99", first)
	}
	if len(fights[1].Players) != 1 || fights[1].Players[0].HasPercent {
		t.Errorf("second fight players = %#v, want one player with unavailable percentile", fights[1].Players)
	}
}

func TestDecodeRankingPercent(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   float64
		wantOK bool
	}{
		{name: "number", raw: `99.4`, want: 99.4, wantOK: true},
		{name: "numeric string", raw: `"88.2"`, want: 88.2, wantOK: true},
		{name: "pending", raw: `"-"`},
		{name: "null", raw: `null`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := decodeRankingPercent(json.RawMessage(tt.raw))
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("decodeRankingPercent(%s) = (%v, %t), want (%v, %t)", tt.raw, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
