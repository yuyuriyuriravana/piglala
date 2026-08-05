package main

import (
	"strings"
	"testing"
	"time"
)

func TestIsRelevantParseFightMatchesZoneAndDifficulty(t *testing.T) {
	zones := []RelevantZone{
		{ZoneID: 73, DifficultyID: 101, ContentType: "Savage"},
		{ZoneID: 67, DifficultyID: 100, ContentType: "Extreme"},
	}
	tests := []struct {
		name  string
		fight ParseFightResult
		want  bool
	}{
		{name: "matching savage", fight: ParseFightResult{ZoneID: 73, DifficultyID: 101}, want: true},
		{name: "matching extreme", fight: ParseFightResult{ZoneID: 67, DifficultyID: 100}, want: true},
		{name: "wrong difficulty", fight: ParseFightResult{ZoneID: 73, DifficultyID: 100}},
		{name: "untracked zone", fight: ParseFightResult{ZoneID: 999, DifficultyID: 101}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRelevantParseFight(tt.fight, zones); got != tt.want {
				t.Fatalf("isRelevantParseFight() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestAggregateTrackedParseFightsMergesReportsAndPlayers(t *testing.T) {
	startedAt := time.Date(2026, 8, 4, 14, 2, 30, 0, time.UTC)
	ivy := WatchedPlayer{Name: "Iyvy Ivy", Server: "ravana", Region: "OC"}
	yuyuri := WatchedPlayer{Name: "Yuyuri Yuri", Server: "ravana", Region: "OC"}
	elara := WatchedPlayer{Name: "Elara Moonshadow", Server: "ravana", Region: "OC"}
	zones := []RelevantZone{{ZoneID: 73, DifficultyID: 101}}

	base := ParseFightResult{
		ReportCode:    "report-a",
		FightID:       7,
		EncounterID:   103,
		EncounterName: "The Tyrant",
		ZoneID:        73,
		DifficultyID:  101,
		StartedAt:     startedAt,
		Players: []ParsePlayerResult{
			{Player: ivy, Job: "Sage", Amount: 24282.643, RankPercent: 99, HasPercent: true},
			{Player: yuyuri, Job: "Viper", Amount: 30986.1, RankPercent: 19, HasPercent: true},
			{Player: WatchedPlayer{Name: "Not Tracked", Server: "Ravana", Region: "OC"}, Job: "Dancer", Amount: 33000, RankPercent: 100, HasPercent: true},
		},
	}
	copy := base
	copy.ReportCode = "report-b"
	copy.FightID = 12
	copy.StartedAt = startedAt.Add(2 * time.Second)
	copy.Players = []ParsePlayerResult{
		{Player: ivy, Job: "Sage", Amount: 24282.643},
		{Player: elara, Job: "Dancer", Amount: 33921.2, RankPercent: 97, HasPercent: true},
	}
	next := base
	next.ReportCode = "report-c"
	next.FightID = 2
	next.StartedAt = startedAt.Add(10 * time.Second)
	next.Players = []ParsePlayerResult{{Player: ivy, Job: "Sage", Amount: 24000, RankPercent: 98, HasPercent: true}}

	got := aggregateTrackedParseFights([]discoveredParseFight{
		{Fight: copy, AnnounceEligible: true},
		{Fight: next, AnnounceEligible: true},
		{Fight: base},
	}, []WatchedPlayer{ivy, yuyuri, elara}, zones)
	if len(got) != 2 {
		t.Fatalf("aggregated fights = %d, want 2", len(got))
	}
	first := got[0]
	if !first.AnnounceEligible {
		t.Fatal("first fight announcement eligible = false, want true")
	}
	if first.Fight.ReportCode != "report-a" || first.Fight.FightID != 7 {
		t.Fatalf("canonical report = %s/%d, want report-a/7", first.Fight.ReportCode, first.Fight.FightID)
	}
	if len(first.Fight.Players) != 3 {
		t.Fatalf("first fight tracked players = %d, want 3", len(first.Fight.Players))
	}
	if first.Fight.Players[1].Player != ivy || !first.Fight.Players[1].HasPercent || first.Fight.Players[1].RankPercent != 99 {
		t.Fatalf("merged Iyvy result = %#v, want percentile-bearing result", first.Fight.Players[1])
	}
}

func TestParseFightResultTemplateRendersTrackedPlayerTable(t *testing.T) {
	messages, err := loadMessageTemplates("templates")
	if err != nil {
		t.Fatalf("loadMessageTemplates: %v", err)
	}

	body, err := messages.ParseFightResult(ParseFightResult{
		ReportCode:    "97tbmq4Yz_xdBFwQ",
		FightID:       49,
		EncounterName: "The Tyrant",
		Players: []ParsePlayerResult{
			{Player: WatchedPlayer{Name: "Iyvy Ivy"}, Job: "Sage", Amount: 24282.643, RankPercent: 99, HasPercent: true},
			{Player: WatchedPlayer{Name: "Yuyuri Yuri"}, Job: "Viper", Amount: 30986.1, RankPercent: 19, HasPercent: true},
			{Player: WatchedPlayer{Name: "Elara Moonshadow"}, Job: "Dancer", Amount: 33921.2},
		},
	})
	if err != nil {
		t.Fatalf("ParseFightResult: %v", err)
	}
	for _, want := range []string{
		"The Tyrant",
		"Player",
		"Job",
		"DPS",
		"Parse",
		"Iyvy Ivy",
		"24,283",
		"99th",
		"Yuyuri Yuri",
		"30,986",
		"19th",
		"Elara Moonshadow",
		"Pending",
		"https://www.fflogs.com/reports/97tbmq4Yz_xdBFwQ#fight=49&type=damage-done",
		"Percentiles are provisional",
		"only be announced once",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Count(body, "Iyvy Ivy") != 1 {
		t.Fatalf("Iyvy row count = %d, want 1:\n%s", strings.Count(body, "Iyvy Ivy"), body)
	}
}
