package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const defaultConversationDBPath = "conversations.sqlite3"

type ConversationStore struct {
	db *sql.DB
}

type ConversationExchange struct {
	MessageID        string
	ChannelID        string
	UserID           string
	UserContent      string
	AssistantContent string
	CreatedAt        time.Time
}

func openConversationStore(path string) (*ConversationStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultConversationDBPath
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open conversation db: %w", err)
	}

	store := &ConversationStore{db: db}
	if err := store.init(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *ConversationStore) Close() error {
	return s.db.Close()
}

func (s *ConversationStore) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
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
`)
	if err != nil {
		return fmt.Errorf("init conversation db: %w", err)
	}
	return nil
}

func (s *ConversationStore) SaveExchange(ctx context.Context, exchange ConversationExchange) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO conversation_exchanges (
	message_id,
	channel_id,
	user_id,
	user_content,
	assistant_content,
	created_at
) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(message_id) DO UPDATE SET
	channel_id = excluded.channel_id,
	user_id = excluded.user_id,
	user_content = excluded.user_content,
	assistant_content = excluded.assistant_content,
	created_at = excluded.created_at
`,
		exchange.MessageID,
		exchange.ChannelID,
		exchange.UserID,
		exchange.UserContent,
		exchange.AssistantContent,
		exchange.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save conversation exchange: %w", err)
	}
	return nil
}
