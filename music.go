package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/snowflake/v2"
)

var (
	errMusicUnavailable = errors.New("music playback is unavailable")
	errNotInVoice       = errors.New("join a voice channel first")
	errNotPlaying       = errors.New("nothing is currently playing")
)

type MusicManager struct {
	session  *discordgo.Session
	address  string
	password string
	secure   bool

	startMu sync.Mutex
	mu      sync.RWMutex
	client  disgolink.Client
	userID  snowflake.ID

	textChannels map[snowflake.ID]string
}

func newMusicManagerFromEnv(session *discordgo.Session) *MusicManager {
	secure, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("LAVALINK_SECURE")))
	return &MusicManager{
		session:      session,
		address:      getenvDefault("LAVALINK_ADDRESS", "127.0.0.1:2333"),
		password:     getenvDefault("LAVALINK_PASSWORD", "youshallnotpass"),
		secure:       secure,
		textChannels: make(map[snowflake.ID]string),
	}
}

func (m *MusicManager) Start(ctx context.Context, userID string) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	m.mu.RLock()
	started := m.client != nil
	m.mu.RUnlock()
	if started {
		return nil
	}

	parsedUserID, err := parseSnowflake(userID)
	if err != nil {
		return fmt.Errorf("parse bot user ID: %w", err)
	}

	client := disgolink.New(
		parsedUserID,
		disgolink.WithListenerFunc(m.onTrackEnd),
		disgolink.WithListenerFunc(m.onTrackException),
		disgolink.WithListenerFunc(m.onTrackStuck),
	)
	if _, err := client.AddNode(ctx, disgolink.NodeConfig{
		Name:     "piglala-local",
		Address:  m.address,
		Password: m.password,
		Secure:   m.secure,
	}); err != nil {
		client.Close()
		return fmt.Errorf("connect to Lavalink at %s: %w", m.address, err)
	}

	m.mu.Lock()
	m.client = client
	m.userID = parsedUserID
	m.mu.Unlock()
	log.Printf("music: connected to Lavalink address=%s secure=%t", m.address, m.secure)
	return nil
}

func (m *MusicManager) ensureStarted(ctx context.Context) error {
	m.mu.RLock()
	started := m.client != nil
	m.mu.RUnlock()
	if started {
		return nil
	}
	if m.session == nil || m.session.State == nil || m.session.State.User == nil {
		return errMusicUnavailable
	}
	if err := m.Start(ctx, m.session.State.User.ID); err != nil {
		return fmt.Errorf("%w: %v", errMusicUnavailable, err)
	}
	return nil
}

func (m *MusicManager) Close() {
	m.mu.Lock()
	client := m.client
	m.client = nil
	m.mu.Unlock()
	if client != nil {
		client.Close()
	}
}

func (m *MusicManager) HandleVoiceStateUpdate(event *discordgo.VoiceStateUpdate) {
	if event == nil || event.VoiceState == nil {
		return
	}

	m.mu.RLock()
	client := m.client
	userID := m.userID
	m.mu.RUnlock()
	if client == nil || event.UserID != userID.String() {
		return
	}

	guildID, err := parseSnowflake(event.GuildID)
	if err != nil {
		log.Printf("music: invalid guild ID in voice state update guild=%q: %v", event.GuildID, err)
		return
	}
	var channelID *snowflake.ID
	if event.ChannelID != "" {
		parsedChannelID, err := parseSnowflake(event.ChannelID)
		if err != nil {
			log.Printf("music: invalid channel ID in voice state update channel=%q: %v", event.ChannelID, err)
			return
		}
		channelID = &parsedChannelID
	} else if client.ExistingPlayer(guildID) == nil {
		return
	}
	client.OnVoiceStateUpdate(context.Background(), guildID, channelID, event.SessionID)
}

func (m *MusicManager) HandleVoiceServerUpdate(event *discordgo.VoiceServerUpdate) {
	if event == nil {
		return
	}

	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return
	}
	guildID, err := parseSnowflake(event.GuildID)
	if err != nil {
		log.Printf("music: invalid guild ID in voice server update guild=%q: %v", event.GuildID, err)
		return
	}
	client.OnVoiceServerUpdate(context.Background(), guildID, event.Token, event.Endpoint)
}

func (m *MusicManager) Play(ctx context.Context, message *discordgo.MessageCreate, rawURL string) (lavalink.Track, error) {
	if message == nil || message.Message == nil || message.Author == nil {
		return lavalink.Track{}, fmt.Errorf("message is incomplete")
	}
	if message.GuildID == "" {
		return lavalink.Track{}, fmt.Errorf("music commands only work in a server")
	}
	if err := m.ensureStarted(ctx); err != nil {
		return lavalink.Track{}, err
	}

	trackURL, err := normalizeYouTubeURL(rawURL)
	if err != nil {
		return lavalink.Track{}, err
	}
	voiceChannelID, err := voiceChannelForUser(m.session, message.GuildID, message.Author.ID)
	if err != nil {
		return lavalink.Track{}, err
	}
	guildID, err := parseSnowflake(message.GuildID)
	if err != nil {
		return lavalink.Track{}, fmt.Errorf("parse guild ID: %w", err)
	}

	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil || client.BestNode() == nil {
		return lavalink.Track{}, errMusicUnavailable
	}

	result, err := client.BestNode().LoadTracks(ctx, trackURL)
	if err != nil {
		return lavalink.Track{}, fmt.Errorf("load YouTube track: %w", err)
	}
	track, err := firstTrack(result)
	if err != nil {
		return lavalink.Track{}, err
	}

	if err := m.session.ChannelVoiceJoinManual(message.GuildID, voiceChannelID, false, true); err != nil {
		return lavalink.Track{}, fmt.Errorf("join voice channel: %w", err)
	}
	player := client.Player(guildID)
	if err := player.Update(ctx, lavalink.WithTrack(track)); err != nil {
		_ = m.session.ChannelVoiceJoinManual(message.GuildID, "", false, false)
		return lavalink.Track{}, fmt.Errorf("start playback: %w", err)
	}

	m.mu.Lock()
	m.textChannels[guildID] = message.ChannelID
	m.mu.Unlock()
	log.Printf(
		"music: playing guild=%s voice_channel=%s text_channel=%s requester=%s title=%q source=%s",
		message.GuildID,
		voiceChannelID,
		message.ChannelID,
		message.Author.ID,
		track.Info.Title,
		track.Info.SourceName,
	)
	return track, nil
}

func (m *MusicManager) Stop(ctx context.Context, guildIDString, userID string) error {
	if guildIDString == "" {
		return fmt.Errorf("music commands only work in a server")
	}
	if err := m.ensureStarted(ctx); err != nil {
		return err
	}
	requesterChannelID, err := voiceChannelForUser(m.session, guildIDString, userID)
	if err != nil {
		return err
	}
	guildID, err := parseSnowflake(guildIDString)
	if err != nil {
		return fmt.Errorf("parse guild ID: %w", err)
	}

	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return errMusicUnavailable
	}
	player := client.ExistingPlayer(guildID)
	if player == nil || player.Track() == nil {
		return errNotPlaying
	}
	if channelID := player.ChannelID(); channelID != nil && channelID.String() != requesterChannelID {
		return fmt.Errorf("join my voice channel before stopping playback")
	}

	if err := player.Update(ctx, lavalink.WithNullTrack()); err != nil {
		return fmt.Errorf("stop playback: %w", err)
	}
	m.disconnect(guildID)
	log.Printf("music: stopped guild=%s requester=%s", guildIDString, userID)
	return nil
}

func (m *MusicManager) disconnect(guildID snowflake.ID) {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client != nil {
		if player := client.ExistingPlayer(guildID); player != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := player.Destroy(ctx); err != nil {
				log.Printf("music: failed to destroy player guild=%s: %v", guildID, err)
			}
			cancel()
		}
	}
	if m.session != nil {
		if err := m.session.ChannelVoiceJoinManual(guildID.String(), "", false, false); err != nil {
			log.Printf("music: failed to leave voice channel guild=%s: %v", guildID, err)
		}
	}

	m.mu.Lock()
	delete(m.textChannels, guildID)
	m.mu.Unlock()
}

func (m *MusicManager) onTrackEnd(_ disgolink.Player, event lavalink.TrackEndEvent) {
	switch event.Reason {
	case lavalink.TrackEndReasonFinished, lavalink.TrackEndReasonLoadFailed, lavalink.TrackEndReasonCleanup:
		go m.disconnect(event.GuildID())
	}
}

func (m *MusicManager) onTrackException(_ disgolink.Player, event lavalink.TrackExceptionEvent) {
	log.Printf("music: track exception guild=%s title=%q: %v", event.GuildID(), event.Track.Info.Title, event.Exception)
	m.notifyPlaybackFailure(event.GuildID(), "Playback failed; I left the voice channel.")
	go m.disconnect(event.GuildID())
}

func (m *MusicManager) onTrackStuck(_ disgolink.Player, event lavalink.TrackStuckEvent) {
	log.Printf("music: track stuck guild=%s title=%q threshold=%s", event.GuildID(), event.Track.Info.Title, event.Threshold)
	m.notifyPlaybackFailure(event.GuildID(), "Playback got stuck; I left the voice channel.")
	go m.disconnect(event.GuildID())
}

func (m *MusicManager) notifyPlaybackFailure(guildID snowflake.ID, message string) {
	m.mu.RLock()
	channelID := m.textChannels[guildID]
	m.mu.RUnlock()
	if channelID == "" || m.session == nil {
		return
	}
	if _, err := m.session.ChannelMessageSend(channelID, message); err != nil {
		log.Printf("music: failed to send playback error guild=%s channel=%s: %v", guildID, channelID, err)
	}
}

func voiceChannelForUser(session *discordgo.Session, guildID, userID string) (string, error) {
	if session == nil || session.State == nil {
		return "", errNotInVoice
	}
	state, err := session.State.VoiceState(guildID, userID)
	if err != nil || state.ChannelID == "" {
		return "", errNotInVoice
	}
	return state.ChannelID, nil
}

func parseSnowflake(value string) (snowflake.ID, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("invalid Discord ID %q", value)
	}
	return snowflake.ID(parsed), nil
}

func normalizeYouTubeURL(raw string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) != 1 {
		return "", fmt.Errorf("usage: !play <YouTube URL>")
	}
	parsed, err := url.Parse(fields[0])
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid YouTube URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("invalid YouTube URL")
	}

	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be", "www.youtube-nocookie.com":
	default:
		return "", fmt.Errorf("only YouTube URLs are supported")
	}

	query := parsed.Query()
	for _, key := range []string{"list", "start_radio", "index", "pp"} {
		query.Del(key)
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String(), nil
}

func firstTrack(result *lavalink.LoadResult) (lavalink.Track, error) {
	if result == nil {
		return lavalink.Track{}, fmt.Errorf("YouTube returned no playable track")
	}
	switch data := result.Data.(type) {
	case lavalink.Track:
		return data, nil
	case lavalink.Playlist:
		if len(data.Tracks) > 0 {
			return data.Tracks[0], nil
		}
	case lavalink.Search:
		if len(data) > 0 {
			return data[0], nil
		}
	case lavalink.Exception:
		return lavalink.Track{}, fmt.Errorf("YouTube track load failed: %w", data)
	}
	return lavalink.Track{}, fmt.Errorf("YouTube returned no playable track")
}
