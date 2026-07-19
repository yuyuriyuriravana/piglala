package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/joho/godotenv"
)

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("startup: loading configuration")
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Fatalf("failed to load .env: %v", err)
	}

	token := strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN"))
	if token == "" {
		log.Fatal("DISCORD_BOT_TOKEN is required")
	}

	fflogsClientID := strings.TrimSpace(os.Getenv("FFLOGS_CLIENT_ID"))
	fflogsClientSecret := strings.TrimSpace(os.Getenv("FFLOGS_CLIENT_SECRET"))
	if fflogsClientID == "" || fflogsClientSecret == "" {
		log.Fatal("FFLOGS_CLIENT_ID and FFLOGS_CLIENT_SECRET are required")
	}

	messages, err := loadMessageTemplates(strings.TrimSpace(os.Getenv("MESSAGE_TEMPLATE_DIR")))
	if err != nil {
		log.Fatalf("failed to load message templates: %v", err)
	}
	log.Printf("templates: loaded from %q", getenvDefault("MESSAGE_TEMPLATE_DIR", "templates"))

	pollInterval := 30 * time.Minute
	if v := strings.TrimSpace(os.Getenv("POLL_INTERVAL_MINUTES")); v != "" {
		if mins, err := strconv.Atoi(v); err == nil && mins > 0 {
			pollInterval = time.Duration(mins) * time.Minute
		}
	}
	storeDBPath := getenvDefault("STORE_DB_PATH", defaultStoreDBPath)
	store, err := loadStore(storeDBPath)
	if err != nil {
		log.Fatalf("failed to load store: %v", err)
	}
	defer store.Close()
	playerCount, bestSetCount, err := store.Stats()
	if err != nil {
		log.Fatalf("failed to read store stats: %v", err)
	}
	log.Printf("store: loaded %d watched player(s) and %d player best set(s) from %s", playerCount, bestSetCount, storeDBPath)

	ffClient := newFFLogsClient(fflogsClientID, fflogsClientSecret)
	llamaClient := newLlamaClientFromEnv()
	convStore, err := openConversationStore(getenvDefault("CONVERSATION_DB_PATH", defaultConversationDBPath))
	if err != nil {
		log.Fatalf("failed to open conversation store: %v", err)
	}
	defer convStore.Close()

	log.Printf("discord: accepting commands from any accessible DM or guild channel")

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("failed to create Discord session: %v", err)
	}
	defer session.Close()
	music := newMusicManagerFromEnv(session)
	defer music.Close()

	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsMessageContent

	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("bot is online as %s#%s", r.User.Username, r.User.Discriminator)
	})
	session.AddHandler(func(_ *discordgo.Session, event *discordgo.VoiceStateUpdate) {
		music.HandleVoiceStateUpdate(event)
	})
	session.AddHandler(func(_ *discordgo.Session, event *discordgo.VoiceServerUpdate) {
		music.HandleVoiceServerUpdate(event)
	})

	session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if !shouldListenToMessage(m) {
			return
		}
		logReceivedDiscordMessage(m)
		if err := store.RecordDiscordMessage(discordMessageLogFromCreate(m)); err != nil {
			log.Printf("store: failed to record discord message message_id=%s channel=%s: %v", m.ID, m.ChannelID, err)
		}

		if m.Author == nil || m.Author.Bot {
			return
		}

		parts := strings.Fields(m.Content)
		if len(parts) == 0 {
			log.Printf("discord: ignored empty DM message_id=%s channel=%s author=%s", m.ID, m.ChannelID, m.Author.ID)
			return
		}
		verb := strings.ToLower(parts[0])
		rest := strings.TrimSpace(strings.TrimPrefix(m.Content, parts[0]))
		log.Printf("discord: received command=%s message_id=%s channel=%s guild=%s author=%s", verb, m.ID, m.ChannelID, m.GuildID, m.Author.ID)

		switch verb {
		case "!help":
			log.Printf("command: !help requested by %s", m.Author.ID)
			replyTemplate(s, m, messages, templateHelp, emptyTemplateData{}, llamaClient, convStore, "help", helpNoteInstructions)

		case "!play":
			if rest == "" {
				sendCommandReply(s, m, "Usage: `!play <YouTube URL>`")
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			track, err := music.Play(ctx, m, rest)
			cancel()
			if err != nil {
				log.Printf("music: !play failed guild=%s channel=%s author=%s: %v", m.GuildID, m.ChannelID, m.Author.ID, err)
				switch {
				case errors.Is(err, errNotInVoice):
					sendCommandReply(s, m, "Join a voice channel first, then send `!play <YouTube URL>`.")
				case errors.Is(err, errMusicUnavailable):
					sendCommandReply(s, m, "Music playback is temporarily unavailable.")
				default:
					sendCommandReply(s, m, "I couldn't play that YouTube URL: "+discordSafeText(err.Error()))
				}
				return
			}
			sendCommandReply(s, m, fmt.Sprintf(
				"Now playing: **%s** (%s)",
				discordSafeText(track.Info.Title),
				formatMusicDuration(track.Info.Length),
			))

		case "!stop":
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := music.Stop(ctx, m.GuildID, m.Author.ID)
			cancel()
			if err != nil {
				log.Printf("music: !stop failed guild=%s channel=%s author=%s: %v", m.GuildID, m.ChannelID, m.Author.ID, err)
				switch {
				case errors.Is(err, errNotInVoice):
					sendCommandReply(s, m, "Join my voice channel first.")
				case errors.Is(err, errNotPlaying):
					sendCommandReply(s, m, "Nothing is currently playing.")
				case errors.Is(err, errMusicUnavailable):
					sendCommandReply(s, m, "Music playback is temporarily unavailable.")
				default:
					sendCommandReply(s, m, "I couldn't stop playback: "+discordSafeText(err.Error()))
				}
				return
			}
			sendCommandReply(s, m, "Playback stopped.")

		case "!status":
			players := store.ListPlayers()
			log.Printf("command: !status requested by %s; watched_players=%d", m.Author.ID, len(players))
			if len(players) == 0 {
				replyTemplate(s, m, messages, templateStatusEmpty, emptyTemplateData{}, llamaClient, convStore, "status", statusNoteInstructions)
				return
			}
			var sb strings.Builder
			for _, p := range players {
				bests := store.GetAllBests(PlayerKey(p))
				playerMsg, err := messages.Render(templateStatusPlayer, playerTemplateData(p))
				if err != nil {
					log.Printf("template: %s: %v", templateStatusPlayer, err)
					return
				}
				sb.WriteString(playerMsg)
				if len(bests) == 0 {
					line, err := messages.Render(templateStatusNoParses, emptyTemplateData{})
					if err != nil {
						log.Printf("template: %s: %v", templateStatusNoParses, err)
						return
					}
					sb.WriteString(line)
				} else {
					wrote, err := writeBestParseStatus(&sb, bests, messages)
					if err != nil {
						log.Printf("template: status parses: %v", err)
						return
					}
					if !wrote {
						line, err := messages.Render(templateStatusNoDisplayedParses, emptyTemplateData{})
						if err != nil {
							log.Printf("template: %s: %v", templateStatusNoDisplayedParses, err)
							return
						}
						sb.WriteString(line)
					}
				}
			}
			if err := replyMessage(s, m, sb.String(), llamaClient, convStore, "status", statusNoteInstructions); err != nil {
				log.Printf("discord: !status response failed for %s: %v", m.Author.ID, err)
				return
			}
			log.Printf("command: !status response sent to %s with %d watched player(s)", m.Author.ID, len(players))

		case "!subscribe":
			targetType, targetID, displayName := subscriptionTargetFromMessage(m)
			added, err := store.AddSubscription(targetType, targetID, displayName)
			if err != nil {
				log.Printf("command: !subscribe failed for target=%s:%s author=%s: %v", targetType, targetID, m.Author.ID, err)
				replyTemplate(s, m, messages, templateSubscribeSaveFailed, emptyTemplateData{}, llamaClient, convStore, "subscription", subscriptionNoteInstructions)
				return
			}
			if added {
				replyTemplate(s, m, messages, templateSubscribeAdded, emptyTemplateData{}, llamaClient, convStore, "subscription", subscriptionNoteInstructions)
				log.Printf("command: !subscribe added target=%s:%s display_name=%q author=%s username=%q", targetType, targetID, displayName, m.Author.ID, m.Author.Username)
			} else {
				replyTemplate(s, m, messages, templateSubscribeAlready, emptyTemplateData{}, llamaClient, convStore, "subscription", subscriptionNoteInstructions)
				log.Printf("command: !subscribe no-op for existing target=%s:%s display_name=%q author=%s username=%q", targetType, targetID, displayName, m.Author.ID, m.Author.Username)
			}

		case "!unsubscribe":
			targetType, targetID, displayName := subscriptionTargetFromMessage(m)
			removed, err := store.RemoveSubscription(targetType, targetID)
			if err != nil {
				log.Printf("command: !unsubscribe failed for target=%s:%s author=%s: %v", targetType, targetID, m.Author.ID, err)
				replyTemplate(s, m, messages, templateUnsubscribeSaveFailed, emptyTemplateData{}, llamaClient, convStore, "subscription", subscriptionNoteInstructions)
				return
			}
			if removed {
				replyTemplate(s, m, messages, templateUnsubscribeRemoved, emptyTemplateData{}, llamaClient, convStore, "subscription", subscriptionNoteInstructions)
				log.Printf("command: !unsubscribe removed target=%s:%s display_name=%q author=%s username=%q", targetType, targetID, displayName, m.Author.ID, m.Author.Username)
			} else {
				replyTemplate(s, m, messages, templateUnsubscribeMissing, emptyTemplateData{}, llamaClient, convStore, "subscription", subscriptionNoteInstructions)
				log.Printf("command: !unsubscribe no-op for missing target=%s:%s display_name=%q author=%s username=%q", targetType, targetID, displayName, m.Author.ID, m.Author.Username)
			}

		case "!watch":
			player, err := parsePlayerArg(rest)
			if err != nil {
				log.Printf("command: !watch invalid arguments from %s: %v", m.Author.ID, err)
				replyTemplate(s, m, messages, templateWatchUsage, emptyTemplateData{}, llamaClient, convStore, "watch", watchNoteInstructions)
				return
			}
			added, err := store.AddPlayer(player)
			if err != nil {
				log.Printf("watch: %v", err)
				replyTemplate(s, m, messages, templateWatchSaveFailed, emptyTemplateData{}, llamaClient, convStore, "watch", watchNoteInstructions)
				return
			}
			if !added {
				replyTemplate(s, m, messages, templateWatchAlready, playerTemplateData(player), llamaClient, convStore, "watch", watchNoteInstructions)
				log.Printf("command: !watch no-op for existing player=%s", PlayerKey(player))
			} else {
				replyTemplate(s, m, messages, templateWatchAdded, playerTemplateData(player), llamaClient, convStore, "watch", watchNoteInstructions)
				log.Printf("command: !watch added player=%s", PlayerKey(player))
			}

		case "!unwatch":
			player, err := parsePlayerArg(rest)
			if err != nil {
				log.Printf("command: !unwatch invalid arguments from %s: %v", m.Author.ID, err)
				replyTemplate(s, m, messages, templateUnwatchUsage, emptyTemplateData{}, llamaClient, convStore, "watch", watchNoteInstructions)
				return
			}
			removed, err := store.RemovePlayer(player)
			if err != nil {
				log.Printf("unwatch: %v", err)
				replyTemplate(s, m, messages, templateUnwatchSaveFailed, emptyTemplateData{}, llamaClient, convStore, "watch", watchNoteInstructions)
				return
			}
			if !removed {
				replyTemplate(s, m, messages, templateUnwatchMissing, playerTemplateData(player), llamaClient, convStore, "watch", watchNoteInstructions)
				log.Printf("command: !unwatch no-op for missing player=%s", PlayerKey(player))
			} else {
				replyTemplate(s, m, messages, templateUnwatchRemoved, playerTemplateData(player), llamaClient, convStore, "watch", watchNoteInstructions)
				log.Printf("command: !unwatch removed player=%s", PlayerKey(player))
			}

		default:
			log.Printf("discord: ignored unsupported command=%s channel=%s author=%s", verb, m.ChannelID, m.Author.ID)
		}
	})

	if err := session.Open(); err != nil {
		log.Fatalf("failed to connect to Discord: %v", err)
	}
	musicStartCtx, musicStartCancel := context.WithTimeout(context.Background(), 15*time.Second)
	if session.State.User == nil {
		log.Printf("music: Discord user state is not ready; playback will connect on the first command")
	} else if err := music.Start(musicStartCtx, session.State.User.ID); err != nil {
		log.Printf("music: startup connection unavailable: %v", err)
	}
	musicStartCancel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poller := &Poller{
		store:    store,
		fflogs:   ffClient,
		session:  session,
		interval: pollInterval,
		messages: messages,
		llama:    llamaClient,
	}
	go poller.Run(ctx)

	log.Printf("bot running, polling every %v", pollInterval)
	waitForShutdown()
	cancel()
}

func parsePlayerArg(s string) (WatchedPlayer, error) {
	matches := regexp.MustCompile(`<([^>]+)>`).FindAllStringSubmatch(s, -1)
	if len(matches) != 3 {
		return WatchedPlayer{}, fmt.Errorf("expected 3 angle-bracket fields, got %d", len(matches))
	}
	name := strings.TrimSpace(matches[0][1])
	server := strings.TrimSpace(matches[1][1])
	region := strings.TrimSpace(matches[2][1])
	if name == "" || server == "" || region == "" {
		return WatchedPlayer{}, fmt.Errorf("name, server, and region must all be non-empty")
	}
	return WatchedPlayer{Name: name, Server: strings.ToLower(server), Region: strings.ToUpper(region)}, nil
}

func waitForShutdown() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutdown signal received")
}

func getenvDefault(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func shouldListenToMessage(m *discordgo.MessageCreate) bool {
	if m == nil {
		return false
	}
	return true
}

func subscriptionTargetFromMessage(m *discordgo.MessageCreate) (string, string, string) {
	if m.GuildID != "" {
		return subscriptionTargetChannel, m.ChannelID, "channel:" + m.ChannelID
	}
	username := ""
	userID := ""
	if m.Author != nil {
		userID = m.Author.ID
		username = m.Author.Username
	}
	return subscriptionTargetUser, userID, username
}

func logReceivedDiscordMessage(m *discordgo.MessageCreate) {
	authorID := ""
	authorUsername := ""
	authorBot := false
	if m.Author != nil {
		authorID = m.Author.ID
		authorUsername = m.Author.Username
		authorBot = m.Author.Bot
	}
	log.Printf(
		"discord: received message_id=%s channel=%s guild=%s author=%s username=%q bot=%t chars=%d content=%q",
		m.ID,
		m.ChannelID,
		m.GuildID,
		authorID,
		authorUsername,
		authorBot,
		len(m.Content),
		m.Content,
	)
}

func discordMessageLogFromCreate(m *discordgo.MessageCreate) DiscordMessageLog {
	entry := DiscordMessageLog{
		MessageID:       m.ID,
		ChannelID:       m.ChannelID,
		GuildID:         m.GuildID,
		Content:         m.Content,
		ReceivedAt:      time.Now().UTC(),
		Attachments:     len(m.Attachments),
		Embeds:          len(m.Embeds),
		MentionedUsers:  len(m.Mentions),
		MentionedRoles:  len(m.MentionRoles),
		MentionEveryone: m.MentionEveryone,
	}
	if !m.Timestamp.IsZero() {
		entry.CreatedAt = m.Timestamp.UTC()
	}
	if m.Author != nil {
		entry.AuthorID = m.Author.ID
		entry.AuthorUsername = m.Author.Username
		entry.AuthorBot = m.Author.Bot
	}
	return entry
}

func sendUserDM(s *discordgo.Session, userID, msg string) error {
	log.Printf("discord: opening DM to user=%s", userID)
	dm, err := s.UserChannelCreate(userID)
	if err != nil {
		return fmt.Errorf("open DM: %w", err)
	}
	if _, err := s.ChannelMessageSend(dm.ID, msg); err != nil {
		return fmt.Errorf("send DM: %w", err)
	}
	log.Printf("discord: sent DM to user=%s channel=%s chars=%d", userID, dm.ID, len(msg))
	return nil
}

func sendCommandReply(s *discordgo.Session, original *discordgo.MessageCreate, message string) {
	if original == nil || original.Message == nil {
		return
	}
	if _, err := s.ChannelMessageSendReply(original.ChannelID, truncateDiscordMessage(message), original.Reference()); err != nil {
		log.Printf("discord: command reply failed channel=%s message_id=%s: %v", original.ChannelID, original.ID, err)
	}
}

func discordSafeText(value string) string {
	value = strings.ReplaceAll(value, "@", "@\u200b")
	value = strings.ReplaceAll(value, "`", "'")
	return strings.TrimSpace(value)
}

func formatMusicDuration(duration lavalink.Duration) string {
	if duration <= 0 {
		return "live"
	}
	totalSeconds := duration.Seconds()
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

const helpNoteInstructions = "Add one short helpful usage note. Do not list all commands again and do not offer multiple rewritten versions."

const statusNoteInstructions = "Add one short observation about the parse status. Do not repeat the full status body, alter any parse values, add headings, or add extra per-player commentary."

const subscriptionNoteInstructions = "Summarize the fixed subscription response in one short sentence. Do not add new instructions, alternatives, roleplay, headings, or extra Discord formatting."

const watchNoteInstructions = "Summarize the fixed watch-list response in one short sentence. Do not add new instructions, alternatives, roleplay, headings, or extra Discord formatting."

func replyMessage(s *discordgo.Session, original *discordgo.MessageCreate, msg string, llama *LlamaClient, convStore *ConversationStore, kind, noteInstructions string) error {
	if original == nil || original.Message == nil {
		return fmt.Errorf("original message is missing")
	}
	userID := ""
	if original.Author != nil {
		userID = original.Author.ID
	}

	finalMsg := appendGeneratedNote(msg, composeLlamaNote(context.Background(), llama, LlamaNoteRequest{
		Kind:            kind,
		RecipientUserID: userID,
		Body:            msg,
		Data: map[string]any{
			"user_message": original.Content,
			"body":         msg,
		},
		Instructions: noteInstructions,
	}))

	if convStore != nil {
		_ = convStore.SaveExchange(context.Background(), ConversationExchange{
			MessageID:        original.ID,
			ChannelID:        original.ChannelID,
			UserID:           userID,
			UserContent:      original.Content,
			AssistantContent: finalMsg,
			CreatedAt:        time.Now(),
		})
	}

	if _, err := s.ChannelMessageSendReply(original.ChannelID, truncateDiscordMessage(finalMsg), original.Reference()); err != nil {
		return fmt.Errorf("send reply: %w", err)
	}
	log.Printf("discord: sent reply channel=%s message_id=%s chars=%d", original.ChannelID, original.ID, len(finalMsg))
	return nil
}

func replyTemplate(s *discordgo.Session, original *discordgo.MessageCreate, messages *MessageTemplates, name string, data any, llama *LlamaClient, convStore *ConversationStore, kind, noteInstructions string) {
	msg, err := messages.Render(name, data)
	if err != nil {
		log.Printf("template: %s: %v", name, err)
		return
	}
	if err := replyMessage(s, original, msg, llama, convStore, kind, noteInstructions); err != nil {
		channelID := ""
		messageID := ""
		if original != nil {
			channelID = original.ChannelID
			messageID = original.ID
		}
		log.Printf("discord: template %s reply failed to channel=%s message_id=%s: %v", name, channelID, messageID, err)
		return
	}
	log.Printf("discord: sent template=%s as reply channel=%s message_id=%s", name, original.ChannelID, original.ID)
}

func playerTemplateData(player WatchedPlayer) PlayerTemplateData {
	return PlayerTemplateData{
		PlayerKey: PlayerKey(player),
		Name:      player.Name,
		Server:    player.Server,
		Region:    player.Region,
	}
}
