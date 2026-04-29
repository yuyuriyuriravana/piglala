package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestSQLiteStorePlayersPersistAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite3")
	store, err := loadStore(path)
	if err != nil {
		t.Fatalf("loadStore: %v", err)
	}

	player := WatchedPlayer{Name: "Yuyuri Yuri", Server: "ravana", Region: "OC"}
	added, err := store.AddPlayer(player)
	if err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}
	if !added {
		t.Fatal("AddPlayer added = false, want true")
	}
	added, err = store.AddPlayer(player)
	if err != nil {
		t.Fatalf("second AddPlayer: %v", err)
	}
	if added {
		t.Fatal("second AddPlayer added = true, want false")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	store, err = loadStore(path)
	if err != nil {
		t.Fatalf("reopen loadStore: %v", err)
	}
	defer store.Close()

	players := store.ListPlayers()
	if len(players) != 1 {
		t.Fatalf("players = %d, want 1", len(players))
	}
	if players[0] != player {
		t.Fatalf("player = %#v, want %#v", players[0], player)
	}
}

func TestSQLiteStoreBestParsesPersistAndUpdate(t *testing.T) {
	store := newTestStore(t)
	player := WatchedPlayer{Name: "Yuyuri Yuri", Server: "ravana", Region: "OC"}
	key := PlayerKey(player)

	if err := store.UpdateBest(key, 101, "Vamp Fatale", 90.1); err != nil {
		t.Fatalf("UpdateBest: %v", err)
	}
	if err := store.UpdateBest(key, 101, "Vamp Fatale", 95.4); err != nil {
		t.Fatalf("second UpdateBest: %v", err)
	}
	if err := store.UpdateBest(key, 102, "Red Hot and Deep Blue", 88.8); err != nil {
		t.Fatalf("third UpdateBest: %v", err)
	}

	best := store.GetBest(key, 101)
	if best.EncounterName != "Vamp Fatale" || best.RankPercent != 95.4 {
		t.Fatalf("best = %#v, want updated Vamp Fatale 95.4", best)
	}

	all := store.GetAllBests(key)
	if len(all) != 2 {
		t.Fatalf("all bests = %d, want 2", len(all))
	}
	if all[102].EncounterName != "Red Hot and Deep Blue" || all[102].RankPercent != 88.8 {
		t.Fatalf("encounter 102 = %#v, want Red Hot and Deep Blue 88.8", all[102])
	}
}

func TestSQLiteStoreRemovePlayerKeepsHistoricalBests(t *testing.T) {
	store := newTestStore(t)
	player := WatchedPlayer{Name: "Yuyuri Yuri", Server: "ravana", Region: "OC"}
	key := PlayerKey(player)

	added, err := store.AddPlayer(player)
	if err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}
	if !added {
		t.Fatal("AddPlayer added = false, want true")
	}
	if err := store.UpdateBest(key, 101, "Vamp Fatale", 90.1); err != nil {
		t.Fatalf("UpdateBest: %v", err)
	}

	removed, err := store.RemovePlayer(player)
	if err != nil {
		t.Fatalf("RemovePlayer: %v", err)
	}
	if !removed {
		t.Fatal("RemovePlayer removed = false, want true")
	}
	if players := store.ListPlayers(); len(players) != 0 {
		t.Fatalf("players = %d, want 0", len(players))
	}
	if best := store.GetBest(key, 101); best.RankPercent != 90.1 {
		t.Fatalf("historical best = %#v, want rank percent 90.1", best)
	}
}

func TestSQLiteStoreStats(t *testing.T) {
	store := newTestStore(t)
	player := WatchedPlayer{Name: "Yuyuri Yuri", Server: "ravana", Region: "OC"}
	if added, err := store.AddPlayer(player); err != nil {
		t.Fatalf("AddPlayer: %v", err)
	} else if !added {
		t.Fatal("AddPlayer added = false, want true")
	}
	if err := store.UpdateBest(PlayerKey(player), 101, "Vamp Fatale", 90.1); err != nil {
		t.Fatalf("UpdateBest: %v", err)
	}

	players, bestSets, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if players != 1 || bestSets != 1 {
		t.Fatalf("Stats = players %d bestSets %d, want 1 1", players, bestSets)
	}
}

func TestSQLiteStoreRecordsTrackedPlayerPollLog(t *testing.T) {
	store := newTestStore(t)
	checkedAt := time.Date(2026, 4, 29, 12, 34, 56, 0, time.UTC)

	err := store.RecordTrackedPlayerPoll(TrackedPlayerPollLog{
		CheckedAt:      checkedAt,
		PlayerKey:      "Yuyuri Yuri@ravana/OC",
		Name:           "Yuyuri Yuri",
		Server:         "ravana",
		Region:         "OC",
		Announce:       true,
		Rankings:       8,
		Updates:        2,
		Failures:       1,
		DurationMillis: 1234,
	})
	if err != nil {
		t.Fatalf("RecordTrackedPlayerPoll: %v", err)
	}

	var got struct {
		CheckedAt      string
		PlayerKey      string
		Name           string
		Server         string
		Region         string
		Announce       int
		Rankings       int
		Updates        int
		Failures       int
		DurationMillis int64
	}
	err = store.db.QueryRow(`
SELECT checked_at, player_key, name, server, region, announce, rankings, updates, failures, duration_millis
FROM tracked_player_poll_logs`).Scan(
		&got.CheckedAt,
		&got.PlayerKey,
		&got.Name,
		&got.Server,
		&got.Region,
		&got.Announce,
		&got.Rankings,
		&got.Updates,
		&got.Failures,
		&got.DurationMillis,
	)
	if err != nil {
		t.Fatalf("query poll log: %v", err)
	}

	if got.CheckedAt != checkedAt.Format(time.RFC3339Nano) {
		t.Fatalf("checked_at = %q, want %q", got.CheckedAt, checkedAt.Format(time.RFC3339Nano))
	}
	if got.PlayerKey != "Yuyuri Yuri@ravana/OC" || got.Name != "Yuyuri Yuri" || got.Server != "ravana" || got.Region != "OC" {
		t.Fatalf("player fields = %#v", got)
	}
	if got.Announce != 1 || got.Rankings != 8 || got.Updates != 2 || got.Failures != 1 || got.DurationMillis != 1234 {
		t.Fatalf("poll counters = %#v", got)
	}
}

func TestSQLiteStoreRecordsDiscordMessageLog(t *testing.T) {
	store := newTestStore(t)
	receivedAt := time.Date(2026, 4, 29, 12, 34, 56, 0, time.UTC)
	createdAt := time.Date(2026, 4, 29, 12, 34, 55, 0, time.UTC)

	err := store.RecordDiscordMessage(DiscordMessageLog{
		MessageID:       "message-1",
		ChannelID:       "channel-1",
		GuildID:         "guild-1",
		AuthorID:        "user-1",
		AuthorUsername:  "Yuyuri",
		AuthorBot:       true,
		Content:         "!status",
		ReceivedAt:      receivedAt,
		CreatedAt:       createdAt,
		Attachments:     1,
		Embeds:          2,
		MentionedUsers:  3,
		MentionedRoles:  4,
		MentionEveryone: true,
	})
	if err != nil {
		t.Fatalf("RecordDiscordMessage: %v", err)
	}

	var got struct {
		MessageID       string
		ChannelID       string
		GuildID         string
		AuthorID        string
		AuthorUsername  string
		AuthorBot       int
		Content         string
		ReceivedAt      string
		CreatedAt       string
		Attachments     int
		Embeds          int
		MentionedUsers  int
		MentionedRoles  int
		MentionEveryone int
	}
	err = store.db.QueryRow(`
SELECT message_id, channel_id, guild_id, author_id, author_username, author_bot, content,
	received_at, created_at, attachments, embeds, mentioned_users, mentioned_roles, mention_everyone
FROM discord_message_logs`).Scan(
		&got.MessageID,
		&got.ChannelID,
		&got.GuildID,
		&got.AuthorID,
		&got.AuthorUsername,
		&got.AuthorBot,
		&got.Content,
		&got.ReceivedAt,
		&got.CreatedAt,
		&got.Attachments,
		&got.Embeds,
		&got.MentionedUsers,
		&got.MentionedRoles,
		&got.MentionEveryone,
	)
	if err != nil {
		t.Fatalf("query discord message log: %v", err)
	}

	if got.MessageID != "message-1" || got.ChannelID != "channel-1" || got.GuildID != "guild-1" {
		t.Fatalf("message fields = %#v", got)
	}
	if got.AuthorID != "user-1" || got.AuthorUsername != "Yuyuri" || got.AuthorBot != 1 {
		t.Fatalf("author fields = %#v", got)
	}
	if got.Content != "!status" || got.ReceivedAt != receivedAt.Format(time.RFC3339Nano) || got.CreatedAt != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("content/time fields = %#v", got)
	}
	if got.Attachments != 1 || got.Embeds != 2 || got.MentionedUsers != 3 || got.MentionedRoles != 4 || got.MentionEveryone != 1 {
		t.Fatalf("message counters = %#v", got)
	}
}

func TestSQLiteStoreSubscribers(t *testing.T) {
	store := newTestStore(t)

	added, err := store.AddSubscription(subscriptionTargetUser, "user-2", "Second")
	if err != nil {
		t.Fatalf("AddSubscription user-2: %v", err)
	}
	if !added {
		t.Fatal("AddSubscription user-2 added = false, want true")
	}
	added, err = store.AddSubscription(subscriptionTargetUser, "user-1", "First")
	if err != nil {
		t.Fatalf("AddSubscription user-1: %v", err)
	}
	if !added {
		t.Fatal("AddSubscription user-1 added = false, want true")
	}
	added, err = store.AddSubscription(subscriptionTargetChannel, "channel-1", "channel:channel-1")
	if err != nil {
		t.Fatalf("AddSubscription channel-1: %v", err)
	}
	if !added {
		t.Fatal("AddSubscription channel-1 added = false, want true")
	}
	added, err = store.AddSubscription(subscriptionTargetUser, "user-2", "Second Renamed")
	if err != nil {
		t.Fatalf("second AddSubscription user-2: %v", err)
	}
	if added {
		t.Fatal("second AddSubscription user-2 added = true, want false")
	}

	got := store.ListNotificationSubscriptions()
	if len(got) != 3 {
		t.Fatalf("subscriptions = %d, want 3 (%v)", len(got), got)
	}
	seen := map[string]bool{}
	for _, subscription := range got {
		seen[subscription.TargetType+":"+subscription.TargetID] = true
	}
	if !seen["user:user-1"] || !seen["user:user-2"] || !seen["channel:channel-1"] {
		t.Fatalf("subscriptions = %v, want user-1, user-2, and channel-1", got)
	}

	var displayName string
	if err := store.db.QueryRow(`
SELECT display_name
FROM notification_subscriptions
WHERE target_type = ? AND target_id = ?`, subscriptionTargetUser, "user-2").Scan(&displayName); err != nil {
		t.Fatalf("query subscription display name: %v", err)
	}
	if displayName != "Second Renamed" {
		t.Fatalf("displayName = %q, want %q", displayName, "Second Renamed")
	}

	removed, err := store.RemoveSubscription(subscriptionTargetUser, "user-2")
	if err != nil {
		t.Fatalf("RemoveSubscription user-2: %v", err)
	}
	if !removed {
		t.Fatal("RemoveSubscription user-2 removed = false, want true")
	}
	removed, err = store.RemoveSubscription(subscriptionTargetUser, "user-2")
	if err != nil {
		t.Fatalf("second RemoveSubscription user-2: %v", err)
	}
	if removed {
		t.Fatal("second RemoveSubscription user-2 removed = true, want false")
	}
	got = store.ListNotificationSubscriptions()
	seen = map[string]bool{}
	for _, subscription := range got {
		seen[subscription.TargetType+":"+subscription.TargetID] = true
	}
	if len(got) != 2 || !seen["user:user-1"] || !seen["channel:channel-1"] {
		t.Fatalf("subscriptions after remove = %v, want user-1 and channel-1", got)
	}
}

func TestDiscordMessageLogFromCreate(t *testing.T) {
	createdAt := time.Date(2026, 4, 29, 12, 34, 55, 0, time.UTC)
	msg := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:              "message-1",
		ChannelID:       "channel-1",
		GuildID:         "guild-1",
		Content:         "!watch <Yuyuri Yuri> <Ravana> <OC>",
		Timestamp:       createdAt,
		MentionEveryone: true,
		Author: &discordgo.User{
			ID:       "user-1",
			Username: "Yuyuri",
			Bot:      true,
		},
		Attachments: []*discordgo.MessageAttachment{{ID: "attachment-1"}},
		Embeds:      []*discordgo.MessageEmbed{{Title: "embed"}},
		Mentions:    []*discordgo.User{{ID: "user-2"}},
		MentionRoles: []string{
			"role-1",
			"role-2",
		},
	}}

	got := discordMessageLogFromCreate(msg)
	if got.MessageID != msg.ID || got.ChannelID != msg.ChannelID || got.GuildID != msg.GuildID {
		t.Fatalf("message fields = %#v", got)
	}
	if got.AuthorID != "user-1" || got.AuthorUsername != "Yuyuri" || !got.AuthorBot {
		t.Fatalf("author fields = %#v", got)
	}
	if got.Content != msg.Content || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("content/time fields = %#v", got)
	}
	if got.Attachments != 1 || got.Embeds != 1 || got.MentionedUsers != 1 || got.MentionedRoles != 2 || !got.MentionEveryone {
		t.Fatalf("counter fields = %#v", got)
	}
	if got.ReceivedAt.IsZero() {
		t.Fatal("ReceivedAt is zero")
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := loadStore(filepath.Join(t.TempDir(), "store.sqlite3"))
	if err != nil {
		t.Fatalf("loadStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	return store
}
