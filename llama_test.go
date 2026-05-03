package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLlamaClientChat(t *testing.T) {
	var got llamaChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"I'm doing well."}}]}`))
	}))
	defer server.Close()

	client := &LlamaClient{
		url:         server.URL,
		maxTokens:   128,
		temperature: 0.7,
		httpClient:  server.Client(),
	}

	reply, err := client.Chat(context.Background(), "Hey how are you doing?")
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if reply != "I'm doing well." {
		t.Fatalf("reply = %q, want %q", reply, "I'm doing well.")
	}
	if len(got.Messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(got.Messages))
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content != "Hey how are you doing?" {
		t.Fatalf("message = %+v", got.Messages[0])
	}
	if got.MaxTokens != 128 {
		t.Fatalf("max_tokens = %d, want 128", got.MaxTokens)
	}
	if got.Stream {
		t.Fatal("stream = true, want false")
	}
	if enableThinking, ok := got.ChatTemplateKwargs["enable_thinking"]; !ok || enableThinking {
		t.Fatalf("enable_thinking = %v, present = %v; want false and present", enableThinking, ok)
	}
}
