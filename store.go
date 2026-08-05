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

CREATE TABLE IF NOT EXISTS parse_run_players (
	player_key TEXT PRIMARY KEY,
	initialized_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS parse_fights (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	encounter_id INTEGER NOT NULL,
	fight_started_at INTEGER NOT NULL,
	encounter_name TEXT NOT NULL,
	report_code TEXT NOT NULL,
	report_fight_id INTEGER NOT NULL,
	announced INTEGER NOT NULL DEFAULT 0,
	observed_at TEXT NOT NULL,
	announced_at TEXT,
	UNIQUE (encounter_id, fight_started_at)
);

CREATE INDEX IF NOT EXISTS idx_parse_fights_encounter_started
ON parse_fights(encounter_id, fight_started_at);

CREATE TABLE IF NOT EXISTS parse_fight_results (
	fight_id INTEGER NOT NULL,
	player_key TEXT NOT NULL,
	player_name TEXT NOT NULL,
	server TEXT NOT NULL,
	region TEXT NOT NULL,
	job TEXT NOT NULL,
	amount REAL NOT NULL,
	rank_percent REAL,
	PRIMARY KEY (fight_id, player_key),
	FOREIGN KEY (fight_id) REFERENCES parse_fights(id) ON DELETE CASCADE
);

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

func (s *Store) ParseRunsInitialized(playerKey string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var initializedAt string
	err := s.db.QueryRow(`
SELECT initialized_at
FROM parse_run_players
WHERE player_key = ?`, playerKey).Scan(&initializedAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check parse run initialization: %w", err)
	}
	return true, nil
}

func (s *Store) MarkParseRunsInitialized(playerKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
INSERT OR IGNORE INTO parse_run_players (player_key, initialized_at)
VALUES (?, ?)`, playerKey, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("mark parse runs initialized: %w", err)
	}
	return nil
}

func (s *Store) RecordParseFight(fight ParseFightResult, suppressAnnouncement bool) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if fight.StartedAt.IsZero() {
		return 0, false, fmt.Errorf("record parse fight: fight start time is required")
	}
	if len(fight.Players) == 0 {
		return 0, false, fmt.Errorf("record parse fight: at least one player result is required")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, false, fmt.Errorf("record parse fight: begin transaction: %w", err)
	}
	defer tx.Rollback()

	fightStartedAt := fight.StartedAt.UTC().Unix()
	var fightID int64
	err = tx.QueryRow(`
SELECT id
FROM parse_fights
WHERE encounter_id = ?
	AND fight_started_at BETWEEN ? AND ?
ORDER BY ABS(fight_started_at - ?)
LIMIT 1`, fight.EncounterID, fightStartedAt-5, fightStartedAt+5, fightStartedAt).Scan(&fightID)
	isNew := false
	if err == sql.ErrNoRows {
		insertResult, insertErr := tx.Exec(`
INSERT INTO parse_fights (
	encounter_id,
	fight_started_at,
	encounter_name,
	report_code,
	report_fight_id,
	announced,
	observed_at
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			fight.EncounterID,
			fightStartedAt,
			fight.EncounterName,
			fight.ReportCode,
			fight.FightID,
			boolToInt(suppressAnnouncement),
			time.Now().UTC().Format(time.RFC3339Nano),
		)
		if insertErr != nil {
			return 0, false, fmt.Errorf("record parse fight: insert fight: %w", insertErr)
		}
		fightID, err = insertResult.LastInsertId()
		if err != nil {
			return 0, false, fmt.Errorf("record parse fight: inserted fight ID: %w", err)
		}
		isNew = true
	} else if err != nil {
		return 0, false, fmt.Errorf("record parse fight: find existing fight: %w", err)
	}

	for _, player := range fight.Players {
		var rankPercent any
		if player.HasPercent {
			rankPercent = player.RankPercent
		}
		if _, err := tx.Exec(`
INSERT OR IGNORE INTO parse_fight_results (
	fight_id,
	player_key,
	player_name,
	server,
	region,
	job,
	amount,
	rank_percent
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			fightID,
			PlayerKey(player.Player),
			player.Player.Name,
			player.Player.Server,
			player.Player.Region,
			player.Job,
			player.Amount,
			rankPercent,
		); err != nil {
			return 0, false, fmt.Errorf("record parse fight: insert player %s: %w", PlayerKey(player.Player), err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, false, fmt.Errorf("record parse fight: commit: %w", err)
	}
	return fightID, isNew, nil
}

func (s *Store) ClaimParseFightAnnouncement(fightID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`
UPDATE parse_fights
SET announced = 1,
	announced_at = ?
WHERE id = ? AND announced = 0`, time.Now().UTC().Format(time.RFC3339Nano), fightID)
	if err != nil {
		return false, fmt.Errorf("claim parse fight announcement: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim parse fight announcement rows affected: %w", err)
	}
	return rows > 0, nil
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
