package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLlamaServerURL = "http://127.0.0.1:8080/v1/chat/completions"
	defaultLlamaMaxTokens = 512
	defaultLlamaTemp      = 0.7
)

type LlamaClient struct {
	url         string
	maxTokens   int
	temperature float64
	httpClient  *http.Client
}

func newLlamaClientFromEnv() *LlamaClient {
	url := strings.TrimSpace(getenvDefault("LLAMA_SERVER_URL", defaultLlamaServerURL))
	maxTokens := getenvPositiveInt("LLAMA_MAX_TOKENS", defaultLlamaMaxTokens)
	temperature := getenvFloat("LLAMA_TEMPERATURE", defaultLlamaTemp)

	return &LlamaClient{
		url:         url,
		maxTokens:   maxTokens,
		temperature: temperature,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (c *LlamaClient) Chat(ctx context.Context, content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("empty message")
	}

	reqBody := llamaChatRequest{
		Messages: []llamaChatMessage{
			{Role: "user", Content: content},
		},
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
		Stream:      false,
		ChatTemplateKwargs: map[string]bool{
			"enable_thinking": false,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("encode llama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build llama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send llama request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read llama response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llama returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var parsed llamaChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode llama response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("llama response had no choices")
	}
	reply := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if reply == "" {
		return "", fmt.Errorf("llama response content was empty")
	}
	return reply, nil
}

type llamaChatRequest struct {
	Messages           []llamaChatMessage `json:"messages"`
	MaxTokens          int                `json:"max_tokens"`
	Temperature        float64            `json:"temperature"`
	Stream             bool               `json:"stream"`
	ChatTemplateKwargs map[string]bool    `json:"chat_template_kwargs"`
}

type llamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llamaChatResponse struct {
	Choices []struct {
		Message llamaChatMessage `json:"message"`
	} `json:"choices"`
}

func getenvPositiveInt(name string, fallback int) int {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func getenvFloat(name string, fallback float64) float64 {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return fallback
}

func truncateDiscordMessage(msg string) string {
	const maxDiscordMessageLength = 2000
	const suffix = "\n\n[response truncated]"
	msg = strings.TrimSpace(msg)
	if len(msg) <= maxDiscordMessageLength {
		return msg
	}
	return msg[:maxDiscordMessageLength-len(suffix)] + suffix
}
