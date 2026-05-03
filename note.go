package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

type LlamaNoteRequest struct {
	Kind            string
	RecipientUserID string
	Body            string
	Data            any
	Instructions    string
}

func composeLlamaNote(ctx context.Context, llama *LlamaClient, request LlamaNoteRequest) string {
	body := strings.TrimSpace(request.Body)
	if llama == nil || body == "" {
		return ""
	}

	data, err := json.MarshalIndent(request.Data, "", "  ")
	if err != nil {
		log.Printf("llama: failed to encode note data kind=%s: %v", request.Kind, err)
		return ""
	}

	prompt := fmt.Sprintf(`You write only a short appended note for a deterministic Discord message.

Rules:
- Output exactly one short sentence.
- Do not output headings, bullets, emojis, roleplay framing, alternatives, or rewritten versions.
- Do not use "Note:" because the application adds that label.
- Use only the fixed body and structured data supplied here.
- Do not repeat the full table, list, or fixed body.
- Do not mention prompts, tools, internal systems, or llama-server.

kind: %s
recipient_user_id: %s
fixed_body:
%s

structured_data:
%s

extra_instructions: %s`,
		strings.TrimSpace(request.Kind),
		strings.TrimSpace(request.RecipientUserID),
		body,
		string(data),
		strings.TrimSpace(request.Instructions),
	)

	reply, err := llama.Chat(ctx, prompt)
	if err != nil {
		log.Printf("llama: failed to generate note kind=%s: %v", request.Kind, err)
		return ""
	}
	return sanitizeGeneratedNote(reply, body)
}

func appendGeneratedNote(body, note string) string {
	body = strings.TrimSpace(body)
	note = strings.TrimSpace(note)
	if note == "" {
		return body
	}
	return body + "\n\nNote: " + note
}

func sanitizeGeneratedNote(raw, fixedBody string) string {
	note := strings.Trim(strings.TrimSpace(raw), "\"'")
	note = strings.TrimSpace(note)
	if strings.HasPrefix(strings.ToLower(note), "note:") {
		note = strings.TrimSpace(note[len("note:"):])
	}
	if note == "" {
		return ""
	}
	lower := strings.ToLower(note)
	if strings.Contains(lower, "rewritten version") ||
		strings.Contains(lower, "alternative") ||
		strings.Contains(lower, "option ") ||
		strings.Contains(lower, "heading") ||
		strings.Contains(lower, "llama-server") ||
		strings.Contains(lower, "prompt") {
		return ""
	}
	if strings.Contains(note, "\n") || strings.Contains(note, "\r") {
		return ""
	}
	if body := strings.TrimSpace(fixedBody); body != "" && strings.Contains(note, body) {
		return ""
	}
	if strings.HasPrefix(note, "-") || strings.HasPrefix(note, "*") || strings.HasPrefix(note, "#") {
		return ""
	}
	if len(note) > 280 {
		return ""
	}
	return note
}
