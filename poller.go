package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

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
	// Populate bests on first run without announcing, then poll on interval.
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

	totalRankings := 0
	updates := 0
	announcements := 0
	failures := 0
	for _, player := range players {
		playerStart := time.Now()
		key := PlayerKey(player)
		playerRankings := 0
		playerUpdates := 0
		playerFailures := 0
		for _, zone := range p.relevantZones {
			log.Printf("fflogs: fetching rankings player=%s zone=%q difficulty=%q content_type=%q", key, zone.ZoneName, zone.DifficultyName, zone.ContentType)
			rankings, err := p.fflogs.GetZoneRankings(player.Name, player.Server, player.Region, zone)
			if err != nil {
				log.Printf("fflogs: %s %s: %v", key, zone.ZoneName, err)
				failures++
				playerFailures++
				continue
			}
			log.Printf("fflogs: fetched %d ranking(s) player=%s zone=%q difficulty=%q content_type=%q", len(rankings), key, zone.ZoneName, zone.DifficultyName, zone.ContentType)
			totalRankings += len(rankings)
			playerRankings += len(rankings)
			for _, r := range rankings {
				if r.RankPercent == 0 {
					continue
				}
				prev := p.store.GetBest(key, r.EncounterID)
				improved := r.BestAmount > prev.BestAmount
				changed := improved || r.RankPercent != prev.RankPercent || r.EncounterName != prev.EncounterName
				if !changed {
					continue
				}
				if err := p.store.UpdateBest(key, r.EncounterID, r.EncounterName, r.RankPercent, r.BestAmount); err != nil {
					log.Printf("store: %s: %v", key, err)
					failures++
					playerFailures++
					continue
				}
				updates++
				playerUpdates++
				shouldAnnounce := announce && improved && prev.BestAmount > 0
				log.Printf("poller: best updated player=%s encounter=%q old_pct=%v new_pct=%v old_amount=%v new_amount=%v improved=%t announce=%t", key, r.EncounterName, prev.RankPercent, r.RankPercent, prev.BestAmount, r.BestAmount, improved, shouldAnnounce)
				if shouldAnnounce {
					p.sendAnnouncement(player, r.EncounterName, prev.RankPercent, r.RankPercent)
					announcements++
				}
			}
		}
		if err := p.store.RecordTrackedPlayerPoll(TrackedPlayerPollLog{
			CheckedAt:      playerStart,
			PlayerKey:      key,
			Name:           player.Name,
			Server:         player.Server,
			Region:         player.Region,
			Announce:       announce,
			Rankings:       playerRankings,
			Updates:        playerUpdates,
			Failures:       playerFailures,
			DurationMillis: time.Since(playerStart).Milliseconds(),
		}); err != nil {
			log.Printf("store: failed to record tracked player poll player=%s: %v", key, err)
			failures++
			playerFailures++
		}
		log.Printf("poller: player check finished player=%s rankings=%d updates=%d failures=%d", key, playerRankings, playerUpdates, playerFailures)
	}
	log.Printf("poller: check finished announce=%t players=%d zones=%d rankings=%d updates=%d announcements=%d failures=%d duration=%v", announce, len(players), len(p.relevantZones), totalRankings, updates, announcements, failures, time.Since(start).Round(time.Millisecond))
}

func (p *Poller) sendAnnouncement(player WatchedPlayer, encounterName string, oldPct, newPct float64) {
	baseMsg, err := p.messages.ParseImprovement(player, encounterName, oldPct, newPct)
	if err != nil {
		log.Printf("template: parse improvement: %v", err)
		return
	}

	msg := appendGeneratedNote(baseMsg, composeLlamaNote(context.Background(), p.llama, LlamaNoteRequest{
		Kind: "parse_improvement",
		Body: baseMsg,
		Data: map[string]any{
			"player_name":    player.Name,
			"server":         player.Server,
			"region":         player.Region,
			"encounter_name": encounterName,
			"previous":       formatPct(oldPct),
			"current":        formatPct(newPct),
		},
		Instructions: "Add one short supportive observation focused on the improvement. Do not rewrite the notification body or invent causes, rankings, streaks, or future outcomes.",
	}))

	subscriptions := p.store.ListNotificationSubscriptions()
	if len(subscriptions) == 0 {
		log.Printf("poller: no subscribers for parse improvement player=%s encounter=%q old=%.1f new=%.1f", PlayerKey(player), encounterName, oldPct, newPct)
		return
	}
	for _, subscription := range subscriptions {
		if err := sendNotification(p.session, subscription, msg); err != nil {
			log.Printf("discord: notification to %s:%s failed: %v", subscription.TargetType, subscription.TargetID, err)
			continue
		}
		log.Printf("poller: announcement sent to %s=%s player=%s encounter=%q old=%.1f new=%.1f", subscription.TargetType, subscription.TargetID, PlayerKey(player), encounterName, oldPct, newPct)
	}
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
	return fmt.Sprintf("%d%s (%v%%)", n, suffix, pct)
}
