package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
)

const recentParseReportLimit = 10

type Poller struct {
	store          *Store
	fflogs         *fflogsClient
	session        *discordgo.Session
	interval       time.Duration
	relevantZones  []RelevantZone
	zonesUpdatedAt time.Time
	messages       *MessageTemplates
	llama          *LlamaClient
}

func (p *Poller) Run(ctx context.Context) {
	// Record recent runs on first startup without announcing, then poll on interval.
	log.Println("poller: starting initial baseline check")
	p.checkAll(false)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("poller: stopped")
			return
		case <-ticker.C:
			log.Println("poller: interval elapsed, starting announced check")
			p.checkAll(true)
		}
	}
}

func (p *Poller) refreshZones() {
	if len(p.relevantZones) > 0 && time.Since(p.zonesUpdatedAt) < 24*time.Hour {
		log.Printf("fflogs: using cached %d zone/difficulty pair(s), age=%v", len(p.relevantZones), time.Since(p.zonesUpdatedAt).Round(time.Second))
		return
	}
	log.Println("fflogs: refreshing relevant zones")
	zones, err := p.fflogs.GetRelevantZones()
	if err != nil {
		log.Printf("fflogs: failed to fetch zones: %v", err)
		return
	}
	p.relevantZones = zones
	p.zonesUpdatedAt = time.Now()
	counts := map[string]int{}
	for _, zone := range zones {
		counts[zone.ContentType]++
	}
	log.Printf("fflogs: tracking %d zone/difficulty pairs savage=%d extreme=%d ultimate=%d", len(zones), counts["Savage"], counts["Extreme"], counts["Ultimate"])
}

func (p *Poller) checkAll(announce bool) {
	start := time.Now()
	p.refreshZones()
	if len(p.relevantZones) == 0 {
		log.Printf("poller: no relevant zones available, skipping")
		return
	}

	players := p.store.ListPlayers()
	log.Printf("poller: check started announce=%t players=%d zones=%d", announce, len(players), len(p.relevantZones))
	if len(players) == 0 {
		log.Printf("poller: check finished announce=%t players=0 zones=%d duration=%v", announce, len(p.relevantZones), time.Since(start).Round(time.Millisecond))
		return
	}

	totalFights := 0
	newFights := 0
	announcements := 0
	failures := 0
	var observations []discoveredParseFight
	for _, player := range players {
		playerStart := time.Now()
		key := PlayerKey(player)
		playerFailures := 0

		initialized, err := p.store.ParseRunsInitialized(key)
		if err != nil {
			log.Printf("store: failed to check parse run baseline player=%s: %v", key, err)
			failures++
			playerFailures++
			continue
		}
		log.Printf("fflogs: fetching recent parse fights player=%s reports=%d", key, recentParseReportLimit)
		fights, err := p.fflogs.GetRecentParseFights(player, recentParseReportLimit)
		if err != nil {
			log.Printf("fflogs: recent parse fights player=%s: %v", key, err)
			failures++
			playerFailures++
			continue
		}
		for _, fight := range fights {
			observations = append(observations, discoveredParseFight{
				Fight:            fight,
				AnnounceEligible: initialized,
			})
		}
		if err := p.store.MarkParseRunsInitialized(key); err != nil {
			log.Printf("store: failed to mark parse run baseline player=%s: %v", key, err)
			failures++
			playerFailures++
		}
		if err := p.store.RecordTrackedPlayerPoll(TrackedPlayerPollLog{
			CheckedAt:      playerStart,
			PlayerKey:      key,
			Name:           player.Name,
			Server:         player.Server,
			Region:         player.Region,
			Announce:       announce,
			Rankings:       len(fights),
			Updates:        0,
			Failures:       playerFailures,
			DurationMillis: time.Since(playerStart).Milliseconds(),
		}); err != nil {
			log.Printf("store: failed to record tracked player poll player=%s: %v", key, err)
			failures++
			playerFailures++
		}
		log.Printf("poller: player discovery finished player=%s parse_fights=%d failures=%d", key, len(fights), playerFailures)
	}

	fights := aggregateTrackedParseFights(observations, players, p.relevantZones)
	for _, pending := range fights {
		fight := pending.Fight
		totalFights++
		suppressAnnouncement := !announce || !pending.AnnounceEligible
		storedFightID, isNew, err := p.store.RecordParseFight(fight, suppressAnnouncement)
		if err != nil {
			log.Printf("store: failed to record parse fight encounter=%q report=%s fight=%d: %v", fight.EncounterName, fight.ReportCode, fight.FightID, err)
			failures++
			continue
		}
		if isNew {
			newFights++
			for _, result := range fight.Players {
				key := PlayerKey(result.Player)
				previousBest := p.store.GetBest(key, fight.EncounterID)
				if result.Amount <= previousBest.BestAmount {
					continue
				}
				percent := previousBest.RankPercent
				if result.HasPercent {
					percent = result.RankPercent
				}
				if err := p.store.UpdateBest(key, fight.EncounterID, fight.EncounterName, percent, result.Amount); err != nil {
					log.Printf("store: failed to update best from parse fight player=%s encounter=%q: %v", key, fight.EncounterName, err)
					failures++
				}
			}
		}

		claimed := false
		if !suppressAnnouncement {
			claimed, err = p.store.ClaimParseFightAnnouncement(storedFightID)
			if err != nil {
				log.Printf("store: failed to claim parse fight announcement id=%d encounter=%q: %v", storedFightID, fight.EncounterName, err)
				failures++
				continue
			}
		}
		log.Printf("poller: parse fight processed encounter=%q report=%s fight=%d started=%s tracked_players=%d new=%t announce=%t", fight.EncounterName, fight.ReportCode, fight.FightID, fight.StartedAt.Format(time.RFC3339), len(fight.Players), isNew, claimed)
		if claimed {
			p.sendParseFightAnnouncement(fight)
			announcements++
		}
	}
	log.Printf("poller: check finished announce=%t players=%d zones=%d observed_fights=%d unique_fights=%d new_fights=%d announcements=%d failures=%d duration=%v", announce, len(players), len(p.relevantZones), len(observations), totalFights, newFights, announcements, failures, time.Since(start).Round(time.Millisecond))
}

type discoveredParseFight struct {
	Fight            ParseFightResult
	AnnounceEligible bool
}

type pendingParseFight struct {
	Fight            ParseFightResult
	AnnounceEligible bool
}

func aggregateTrackedParseFights(observations []discoveredParseFight, trackedPlayers []WatchedPlayer, zones []RelevantZone) []pendingParseFight {
	sort.SliceStable(observations, func(i, j int) bool {
		if observations[i].Fight.StartedAt.Equal(observations[j].Fight.StartedAt) {
			return observations[i].Fight.ReportCode < observations[j].Fight.ReportCode
		}
		return observations[i].Fight.StartedAt.Before(observations[j].Fight.StartedAt)
	})

	var out []pendingParseFight
	for _, observation := range observations {
		if !isRelevantParseFight(observation.Fight, zones) {
			continue
		}

		trackedResults := make([]ParsePlayerResult, 0, len(observation.Fight.Players))
		for _, result := range observation.Fight.Players {
			tracked, ok := findTrackedPlayer(trackedPlayers, result.Player)
			if !ok {
				continue
			}
			result.Player = tracked
			trackedResults = append(trackedResults, result)
		}
		if len(trackedResults) == 0 {
			continue
		}

		index := matchingParseFight(out, observation.Fight)
		if index < 0 {
			fight := observation.Fight
			fight.Players = nil
			out = append(out, pendingParseFight{Fight: fight})
			index = len(out) - 1
		}
		out[index].AnnounceEligible = out[index].AnnounceEligible || observation.AnnounceEligible
		for _, result := range trackedResults {
			mergeParsePlayerResult(&out[index].Fight.Players, result)
		}
	}

	for i := range out {
		sort.Slice(out[i].Fight.Players, func(a, b int) bool {
			return PlayerKey(out[i].Fight.Players[a].Player) < PlayerKey(out[i].Fight.Players[b].Player)
		})
	}
	return out
}

func matchingParseFight(fights []pendingParseFight, candidate ParseFightResult) int {
	for i := range fights {
		fight := fights[i].Fight
		if fight.EncounterID == candidate.EncounterID && absDuration(fight.StartedAt.Sub(candidate.StartedAt)) <= 5*time.Second {
			return i
		}
	}
	return -1
}

func mergeParsePlayerResult(results *[]ParsePlayerResult, candidate ParsePlayerResult) {
	for i := range *results {
		if !sameFFLogsCharacter((*results)[i].Player, candidate.Player.Name, candidate.Player.Server, candidate.Player.Region) {
			continue
		}
		if !(*results)[i].HasPercent && candidate.HasPercent {
			(*results)[i] = candidate
		}
		return
	}
	*results = append(*results, candidate)
}

func findTrackedPlayer(players []WatchedPlayer, candidate WatchedPlayer) (WatchedPlayer, bool) {
	for _, player := range players {
		if sameFFLogsCharacter(player, candidate.Name, candidate.Server, candidate.Region) {
			return player, true
		}
	}
	return WatchedPlayer{}, false
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func isRelevantParseFight(fight ParseFightResult, zones []RelevantZone) bool {
	for _, zone := range zones {
		if fight.ZoneID == zone.ZoneID && fight.DifficultyID == zone.DifficultyID {
			return true
		}
	}
	return false
}

func (p *Poller) sendParseFightAnnouncement(fight ParseFightResult) {
	baseMsg, err := p.messages.ParseFightResult(fight)
	if err != nil {
		log.Printf("template: parse fight result: %v", err)
		return
	}

	subscriptions := p.store.ListNotificationSubscriptions()
	if len(subscriptions) == 0 {
		log.Printf("poller: no subscribers for parse fight encounter=%q report=%s fight=%d", fight.EncounterName, fight.ReportCode, fight.FightID)
		return
	}
	for _, subscription := range subscriptions {
		if err := sendNotification(p.session, subscription, baseMsg); err != nil {
			log.Printf("discord: notification to %s:%s failed: %v", subscription.TargetType, subscription.TargetID, err)
			continue
		}
		log.Printf("poller: parse fight announcement sent to %s=%s encounter=%q report=%s fight=%d tracked_players=%d", subscription.TargetType, subscription.TargetID, fight.EncounterName, fight.ReportCode, fight.FightID, len(fight.Players))
	}
}

func formatDPS(amount float64) string {
	return formatGil(int(math.Round(amount)))
}

func sendNotification(s *discordgo.Session, subscription NotificationSubscription, msg string) error {
	switch subscription.TargetType {
	case subscriptionTargetUser:
		return sendUserDM(s, subscription.TargetID, msg)
	case subscriptionTargetChannel:
		if _, err := s.ChannelMessageSend(subscription.TargetID, msg); err != nil {
			return fmt.Errorf("send channel message: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported notification target type %q", subscription.TargetType)
	}
}

func formatPct(pct float64) string {
	n := int(pct)
	suffix := "th"
	switch n % 10 {
	case 1:
		if n%100 != 11 {
			suffix = "st"
		}
	case 2:
		if n%100 != 12 {
			suffix = "nd"
		}
	case 3:
		if n%100 != 13 {
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s (%.1f%%)", n, suffix, pct)
}
