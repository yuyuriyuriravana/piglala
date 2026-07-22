package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreSaveExchange(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite3")
	store, err := loadStore(dbPath)
	if err != nil {
		t.Fatalf("loadStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	exchange := ConversationExchange{
		MessageID:        "message-1",
		ChannelID:        "channel-1",
		UserID:           "user-1",
		UserContent:      "Hey how are you doing?",
		AssistantContent: "I'm doing well.",
		CreatedAt:        time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC),
	}
	if err := store.SaveExchange(ctx, exchange); err != nil {
		t.Fatalf("SaveExchange: %v", err)
	}

	got := readConversationExchange(t, store.db, "message-1")
	if got.UserContent != exchange.UserContent {
		t.Fatalf("user_content = %q, want %q", got.UserContent, exchange.UserContent)
	}
	if got.AssistantContent != exchange.AssistantContent {
		t.Fatalf("assistant_content = %q, want %q", got.AssistantContent, exchange.AssistantContent)
	}
	if got.ChannelID != exchange.ChannelID {
		t.Fatalf("channel_id = %q, want %q", got.ChannelID, exchange.ChannelID)
	}
	if got.UserID != exchange.UserID {
		t.Fatalf("user_id = %q, want %q", got.UserID, exchange.UserID)
	}
}

func TestStoreSaveExchangeUpsertsMessage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite3")
	store, err := loadStore(dbPath)
	if err != nil {
		t.Fatalf("loadStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	exchange := ConversationExchange{
		MessageID:        "message-1",
		ChannelID:        "channel-1",
		UserID:           "user-1",
		UserContent:      "first",
		AssistantContent: "first reply",
		CreatedAt:        time.Now(),
	}
	if err := store.SaveExchange(ctx, exchange); err != nil {
		t.Fatalf("SaveExchange first: %v", err)
	}
	exchange.UserContent = "edited"
	exchange.AssistantContent = "edited reply"
	if err := store.SaveExchange(ctx, exchange); err != nil {
		t.Fatalf("SaveExchange second: %v", err)
	}

	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM conversation_exchanges`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1", count)
	}

	got := readConversationExchange(t, store.db, "message-1")
	if got.UserContent != "edited" {
		t.Fatalf("user_content = %q, want edited", got.UserContent)
	}
	if got.AssistantContent != "edited reply" {
		t.Fatalf("assistant_content = %q, want edited reply", got.AssistantContent)
	}
}

func readConversationExchange(t *testing.T, db *sql.DB, messageID string) ConversationExchange {
	t.Helper()

	var exchange ConversationExchange
	var createdAt string
	err := db.QueryRow(`
SELECT message_id, channel_id, user_id, user_content, assistant_content, created_at
FROM conversation_exchanges
WHERE message_id = ?
`, messageID).Scan(
		&exchange.MessageID,
		&exchange.ChannelID,
		&exchange.UserID,
		&exchange.UserContent,
		&exchange.AssistantContent,
		&createdAt,
	)
	if err != nil {
		t.Fatalf("read exchange: %v", err)
	}
	return exchange
}
