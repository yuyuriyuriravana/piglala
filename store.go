package main

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const defaultStoreDBPath = "store.sqlite3"

const (
	subscriptionTargetUser    = "user"
	subscriptionTargetChannel = "channel"
)

type WatchedPlayer struct {
	Name   string
	Server string
	Region string
}

func PlayerKey(p WatchedPlayer) string {
	return fmt.Sprintf("%s@%s/%s", p.Name, p.Server, p.Region)
}

type BestParse struct {
	EncounterName string
	RankPercent   float64
	BestAmount    float64
}

type TrackedPlayerPollLog struct {
	CheckedAt      time.Time
	PlayerKey      string
	Name           string
	Server         string
	Region         string
	Announce       bool
	Rankings       int
	Updates        int
	Failures       int
	DurationMillis int64
}

type DiscordMessageLog struct {
	MessageID       string
	ChannelID       string
	GuildID         string
	AuthorID        string
	AuthorUsername  string
	AuthorBot       bool
	Content         string
	ReceivedAt      time.Time
	CreatedAt       time.Time
	Attachments     int
	Embeds          int
	MentionedUsers  int
	MentionedRoles  int
	MentionEveryone bool
}

type NotificationSubscription struct {
	TargetType string
	TargetID   string
}

type Store struct {
	mu   sync.Mutex
	path string
	db   *sql.DB
}

func loadStore(path string) (*Store, error) {
	if path == "" {
		path = defaultStoreDBPath
	}
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open store db: %w", err)
	}
	s := &Store{path: path, db: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) init() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS watched_players (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	player_key TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	server TEXT NOT NULL,
	region TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS best_parses (
	player_key TEXT NOT NULL,
	encounter_id INTEGER NOT NULL,
	encounter_name TEXT NOT NULL,
	rank_percent REAL NOT NULL,
	best_amount REAL NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (player_key, encounter_id)
);

CREATE TABLE IF NOT EXISTS tracked_player_poll_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	checked_at TEXT NOT NULL,
	player_key TEXT NOT NULL,
	name TEXT NOT NULL,
	server TEXT NOT NULL,
	region TEXT NOT NULL,
	announce INTEGER NOT NULL,
	rankings INTEGER NOT NULL,
	updates INTEGER NOT NULL,
	failures INTEGER NOT NULL,
	duration_millis INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tracked_player_poll_logs_player_checked
ON tracked_player_poll_logs(player_key, checked_at);

CREATE TABLE IF NOT EXISTS discord_message_logs (
	message_id TEXT PRIMARY KEY,
	channel_id TEXT NOT NULL,
	guild_id TEXT NOT NULL,
	author_id TEXT NOT NULL,
	author_username TEXT NOT NULL,
	author_bot INTEGER NOT NULL,
	content TEXT NOT NULL,
	received_at TEXT NOT NULL,
	created_at TEXT NOT NULL,
	attachments INTEGER NOT NULL,
	embeds INTEGER NOT NULL,
	mentioned_users INTEGER NOT NULL,
	mentioned_roles INTEGER NOT NULL,
	mention_everyone INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_discord_message_logs_received_at
ON discord_message_logs(received_at);

CREATE INDEX IF NOT EXISTS idx_discord_message_logs_channel_received
ON discord_message_logs(channel_id, received_at);

CREATE TABLE IF NOT EXISTS conversation_exchanges (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id TEXT NOT NULL UNIQUE,
	channel_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	user_content TEXT NOT NULL,
	assistant_content TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_conversation_exchanges_channel_created
ON conversation_exchanges(channel_id, created_at);

CREATE INDEX IF NOT EXISTS idx_conversation_exchanges_user_created
ON conversation_exchanges(user_id, created_at);

CREATE TABLE IF NOT EXISTS notification_subscribers (
	user_id TEXT PRIMARY KEY,
	username TEXT NOT NULL,
	subscribed_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_subscriptions (
	target_type TEXT NOT NULL,
	target_id TEXT NOT NULL,
	display_name TEXT NOT NULL,
	subscribed_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (target_type, target_id)
);

CREATE TABLE IF NOT EXISTS item_catalog (
	item_id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	normalized_name TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_item_catalog_normalized_name
ON item_catalog(normalized_name);

CREATE TABLE IF NOT EXISTS item_catalog_metadata (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

INSERT OR IGNORE INTO notification_subscriptions (
	target_type,
	target_id,
	display_name,
	subscribed_at,
	updated_at
)
SELECT
	'user',
	user_id,
	username,
	subscribed_at,
	updated_at
FROM notification_subscribers;
`)
	if err != nil {
		return fmt.Errorf("init store db: %w", err)
	}

	if _, err := s.db.Exec(`ALTER TABLE best_parses ADD COLUMN best_amount REAL NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migrate best_parses.best_amount: %w", err)
		}
	}
	return nil
}

func (s *Store) Stats() (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var players int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM watched_players`).Scan(&players); err != nil {
		return 0, 0, fmt.Errorf("count watched players: %w", err)
	}
	var bestSets int
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT player_key) FROM best_parses`).Scan(&bestSets); err != nil {
		return 0, 0, fmt.Errorf("count best sets: %w", err)
	}
	return players, bestSets, nil
}

func (s *Store) AddPlayer(p WatchedPlayer) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`
INSERT OR IGNORE INTO watched_players (player_key, name, server, region, created_at)
VALUES (?, ?, ?, ?, ?)`,
		PlayerKey(p), p.Name, p.Server, p.Region, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, fmt.Errorf("add watched player: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("add watched player rows affected: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) RemovePlayer(p WatchedPlayer) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM watched_players WHERE player_key = ?`, PlayerKey(p))
	if err != nil {
		return false, fmt.Errorf("remove watched player: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("remove watched player rows affected: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) ListPlayers() []WatchedPlayer {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT name, server, region FROM watched_players ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []WatchedPlayer
	for rows.Next() {
		var p WatchedPlayer
		if err := rows.Scan(&p.Name, &p.Server, &p.Region); err != nil {
			return nil
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return out
}

func (s *Store) GetBest(playerKey string, encounterID int) BestParse {
	s.mu.Lock()
	defer s.mu.Unlock()

	var best BestParse
	err := s.db.QueryRow(`
SELECT encounter_name, rank_percent, best_amount
FROM best_parses
WHERE player_key = ? AND encounter_id = ?`, playerKey, encounterID).Scan(&best.EncounterName, &best.RankPercent, &best.BestAmount)
	if err != nil {
		return BestParse{}
	}
	return best
}

func (s *Store) GetAllBests(playerKey string) map[int]BestParse {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
SELECT encounter_id, encounter_name, rank_percent, best_amount
FROM best_parses
WHERE player_key = ?`, playerKey)
	if err != nil {
		return map[int]BestParse{}
	}
	defer rows.Close()

	out := make(map[int]BestParse)
	for rows.Next() {
		var encounterID int
		var best BestParse
		if err := rows.Scan(&encounterID, &best.EncounterName, &best.RankPercent, &best.BestAmount); err != nil {
			return map[int]BestParse{}
		}
		out[encounterID] = best
	}
	if err := rows.Err(); err != nil {
		return map[int]BestParse{}
	}
	return out
}

func (s *Store) UpdateBest(playerKey string, encounterID int, encounterName string, pct, bestAmount float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
INSERT INTO best_parses (player_key, encounter_id, encounter_name, rank_percent, best_amount, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(player_key, encounter_id) DO UPDATE SET
	encounter_name = excluded.encounter_name,
	rank_percent = excluded.rank_percent,
	best_amount = excluded.best_amount,
	updated_at = excluded.updated_at`,
		playerKey, encounterID, encounterName, pct, bestAmount, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("update best parse: %w", err)
	}
	return nil
}

func (s *Store) RecordTrackedPlayerPoll(logEntry TrackedPlayerPollLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	checkedAt := logEntry.CheckedAt.UTC()
	if checkedAt.IsZero() {
		checkedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`
INSERT INTO tracked_player_poll_logs (
	checked_at,
	player_key,
	name,
	server,
	region,
	announce,
	rankings,
	updates,
	failures,
	duration_millis
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		checkedAt.Format(time.RFC3339Nano),
		logEntry.PlayerKey,
		logEntry.Name,
		logEntry.Server,
		logEntry.Region,
		boolToInt(logEntry.Announce),
		logEntry.Rankings,
		logEntry.Updates,
		logEntry.Failures,
		logEntry.DurationMillis,
	)
	if err != nil {
		return fmt.Errorf("record tracked player poll: %w", err)
	}
	return nil
}

func (s *Store) RecordDiscordMessage(logEntry DiscordMessageLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	receivedAt := logEntry.ReceivedAt.UTC()
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	createdAt := logEntry.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = receivedAt
	}

	_, err := s.db.Exec(`
INSERT INTO discord_message_logs (
	message_id,
	channel_id,
	guild_id,
	author_id,
	author_username,
	author_bot,
	content,
	received_at,
	created_at,
	attachments,
	embeds,
	mentioned_users,
	mentioned_roles,
	mention_everyone
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(message_id) DO UPDATE SET
	channel_id = excluded.channel_id,
	guild_id = excluded.guild_id,
	author_id = excluded.author_id,
	author_username = excluded.author_username,
	author_bot = excluded.author_bot,
	content = excluded.content,
	received_at = excluded.received_at,
	created_at = excluded.created_at,
	attachments = excluded.attachments,
	embeds = excluded.embeds,
	mentioned_users = excluded.mentioned_users,
	mentioned_roles = excluded.mentioned_roles,
	mention_everyone = excluded.mention_everyone`,
		logEntry.MessageID,
		logEntry.ChannelID,
		logEntry.GuildID,
		logEntry.AuthorID,
		logEntry.AuthorUsername,
		boolToInt(logEntry.AuthorBot),
		logEntry.Content,
		receivedAt.Format(time.RFC3339Nano),
		createdAt.Format(time.RFC3339Nano),
		logEntry.Attachments,
		logEntry.Embeds,
		logEntry.MentionedUsers,
		logEntry.MentionedRoles,
		boolToInt(logEntry.MentionEveryone),
	)
	if err != nil {
		return fmt.Errorf("record discord message: %w", err)
	}
	return nil
}

func (s *Store) AddSubscription(targetType, targetID, displayName string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var existingTargetID string
	err := s.db.QueryRow(`
SELECT target_id
FROM notification_subscriptions
WHERE target_type = ? AND target_id = ?`, targetType, targetID).Scan(&existingTargetID)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("check subscription: %w", err)
	}
	added := err == sql.ErrNoRows

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if added {
		_, err = s.db.Exec(`
INSERT INTO notification_subscriptions (target_type, target_id, display_name, subscribed_at, updated_at)
VALUES (?, ?, ?, ?, ?)`, targetType, targetID, displayName, now, now)
	} else {
		_, err = s.db.Exec(`
UPDATE notification_subscriptions
SET display_name = ?, updated_at = ?
WHERE target_type = ? AND target_id = ?`, displayName, now, targetType, targetID)
	}
	if err != nil {
		return false, fmt.Errorf("save subscription: %w", err)
	}
	return added, nil
}

func (s *Store) RemoveSubscription(targetType, targetID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`
DELETE FROM notification_subscriptions
WHERE target_type = ? AND target_id = ?`, targetType, targetID)
	if err != nil {
		return false, fmt.Errorf("remove subscription: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("remove subscription rows affected: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) ListNotificationSubscriptions() []NotificationSubscription {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
SELECT target_type, target_id
FROM notification_subscriptions
ORDER BY subscribed_at, target_type, target_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []NotificationSubscription
	for rows.Next() {
		var subscription NotificationSubscription
		if err := rows.Scan(&subscription.TargetType, &subscription.TargetID); err != nil {
			return nil
		}
		out = append(out, subscription)
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return out
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
