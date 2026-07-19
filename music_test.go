package main

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/disgoorg/disgolink/v3/lavalink"
)

func TestNormalizeYouTubeURLStripsRadioPlaylist(t *testing.T) {
	got, err := normalizeYouTubeURL("https://www.youtube.com/watch?v=XFgpi7vynko&list=RDXFgpi7vynko&start_radio=1")
	if err != nil {
		t.Fatalf("normalizeYouTubeURL() error = %v", err)
	}
	if got != "https://www.youtube.com/watch?v=XFgpi7vynko" {
		t.Fatalf("normalizeYouTubeURL() = %q", got)
	}
}

func TestNormalizeYouTubeURLAcceptsShortURL(t *testing.T) {
	got, err := normalizeYouTubeURL("https://youtu.be/XFgpi7vynko?t=42")
	if err != nil {
		t.Fatalf("normalizeYouTubeURL() error = %v", err)
	}
	if got != "https://youtu.be/XFgpi7vynko?t=42" {
		t.Fatalf("normalizeYouTubeURL() = %q", got)
	}
}

func TestNormalizeYouTubeURLRejectsUnsupportedInput(t *testing.T) {
	for _, input := range []string{
		"",
		"https://example.com/video",
		"file:///etc/passwd",
		"https://youtube.com/watch?v=one https://youtube.com/watch?v=two",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := normalizeYouTubeURL(input); err == nil {
				t.Fatalf("normalizeYouTubeURL(%q) unexpectedly succeeded", input)
			}
		})
	}
}

func TestFirstTrack(t *testing.T) {
	want := lavalink.Track{Info: lavalink.TrackInfo{Title: "Test track"}}
	tests := []struct {
		name   string
		result *lavalink.LoadResult
	}{
		{name: "track", result: &lavalink.LoadResult{Data: want}},
		{name: "playlist", result: &lavalink.LoadResult{Data: lavalink.Playlist{Tracks: []lavalink.Track{want}}}},
		{name: "search", result: &lavalink.LoadResult{Data: lavalink.Search{want}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := firstTrack(tt.result)
			if err != nil {
				t.Fatalf("firstTrack() error = %v", err)
			}
			if got.Info.Title != want.Info.Title {
				t.Fatalf("firstTrack() title = %q", got.Info.Title)
			}
		})
	}
}

func TestFirstTrackRejectsEmptyResult(t *testing.T) {
	for _, result := range []*lavalink.LoadResult{
		nil,
		{Data: lavalink.Empty{}},
		{Data: lavalink.Playlist{}},
		{Data: lavalink.Search{}},
	} {
		if _, err := firstTrack(result); err == nil {
			t.Fatal("firstTrack() unexpectedly succeeded")
		}
	}
}

func TestVoiceChannelForUser(t *testing.T) {
	state := discordgo.NewState()
	if err := state.GuildAdd(&discordgo.Guild{
		ID: "123",
		VoiceStates: []*discordgo.VoiceState{
			{GuildID: "123", ChannelID: "456", UserID: "789"},
		},
	}); err != nil {
		t.Fatalf("GuildAdd() error = %v", err)
	}
	session := &discordgo.Session{State: state}

	got, err := voiceChannelForUser(session, "123", "789")
	if err != nil {
		t.Fatalf("voiceChannelForUser() error = %v", err)
	}
	if got != "456" {
		t.Fatalf("voiceChannelForUser() = %q", got)
	}
	if _, err := voiceChannelForUser(session, "123", "missing"); err != errNotInVoice {
		t.Fatalf("voiceChannelForUser() error = %v, want errNotInVoice", err)
	}
}

func TestFormatMusicDuration(t *testing.T) {
	tests := map[lavalink.Duration]string{
		0:                      "live",
		65 * lavalink.Second:   "1:05",
		3661 * lavalink.Second: "1:01:01",
		2*lavalink.Hour + 3*lavalink.Minute + 4*lavalink.Second: "2:03:04",
	}
	for input, want := range tests {
		if got := formatMusicDuration(input); got != want {
			t.Errorf("formatMusicDuration(%s) = %q, want %q", input, got, want)
		}
	}
}

func TestDiscordSafeTextDisablesMentionsAndMarkdownCode(t *testing.T) {
	got := discordSafeText("@everyone `track`")
	if strings.Contains(got, "@everyone") || strings.Contains(got, "`") {
		t.Fatalf("discordSafeText() = %q", got)
	}
}
