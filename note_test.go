package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComposeLlamaNoteUsesFixedBodyAndData(t *testing.T) {
	var got llamaChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"The top parse is already excellent, so the next gains are refinement work."}}]}`))
	}))
	defer server.Close()

	llama := &LlamaClient{
		url:         server.URL,
		maxTokens:   128,
		temperature: 0.7,
		httpClient:  server.Client(),
	}
	body := "**Iyvy Ivy** (ravana, OC)\n  **Heavyweight (M9S-M12S)**\n    Vamp Fatale: 99th (99.9%)"
	note := composeLlamaNote(context.Background(), llama, LlamaNoteRequest{
		Kind:            "status",
		RecipientUserID: "user-1",
		Body:            body,
		Data:            map[string]string{"command": "!status"},
		Instructions:    statusNoteInstructions,
	})

	if note != "The top parse is already excellent, so the next gains are refinement work." {
		t.Fatalf("note = %q", note)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(got.Messages))
	}
	prompt := got.Messages[0].Content
	for _, want := range []string{"fixed_body:", body, `"command": "!status"`, statusNoteInstructions, "Do not output headings", "untrusted facts"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestComposeLlamaNoteFallsBackForUnusableOutput(t *testing.T) {
	tests := []struct {
		name  string
		reply string
	}{
		{name: "empty", reply: ""},
		{name: "multi line", reply: "Good.\nOption 2: Different."},
		{name: "rewrite framing", reply: "Here is a rewritten version of the response."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + jsonTestString(tt.reply) + `}}]}`))
			}))
			defer server.Close()

			llama := &LlamaClient{
				url:         server.URL,
				maxTokens:   128,
				temperature: 0.7,
				httpClient:  server.Client(),
			}
			got := composeLlamaNote(context.Background(), llama, LlamaNoteRequest{
				Kind: "help",
				Body: "Supported commands:\n`!help`",
				Data: map[string]string{"command": "!help"},
			})
			if got != "" {
				t.Fatalf("note = %q, want empty", got)
			}
		})
	}
}

func jsonTestString(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `""`
	}
	return string(b)
}

func TestAppendGeneratedNote(t *testing.T) {
	if got := appendGeneratedNote("Body", "Short note."); got != "Body\n\nNote: Short note." {
		t.Fatalf("appendGeneratedNote = %q", got)
	}
	if got := appendGeneratedNote("Body", " "); got != "Body" {
		t.Fatalf("appendGeneratedNote without note = %q", got)
	}
}
