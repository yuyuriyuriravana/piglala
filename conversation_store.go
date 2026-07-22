package main

import (
	"context"
	"fmt"
	"time"
)

type ConversationExchange struct {
	MessageID        string
	ChannelID        string
	UserID           string
	UserContent      string
	AssistantContent string
	CreatedAt        time.Time
}

func (s *Store) SaveExchange(ctx context.Context, exchange ConversationExchange) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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
