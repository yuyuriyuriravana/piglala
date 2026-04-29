package main

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestShouldListenToMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  *discordgo.MessageCreate
		want bool
	}{
		{
			name: "dm",
			msg: &discordgo.MessageCreate{Message: &discordgo.Message{
				ChannelID: "dm-channel",
			}},
			want: true,
		},
		{
			name: "guild channel",
			msg: &discordgo.MessageCreate{Message: &discordgo.Message{
				GuildID:   "guild-1",
				ChannelID: "channel-1",
			}},
			want: true,
		},
		{
			name: "another guild channel",
			msg: &discordgo.MessageCreate{Message: &discordgo.Message{
				GuildID:   "guild-1",
				ChannelID: "other-channel",
			}},
			want: true,
		},
		{
			name: "nil message",
			msg:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldListenToMessage(tt.msg); got != tt.want {
				t.Fatalf("shouldListenToMessage() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestSubscriptionTargetFromMessage(t *testing.T) {
	user := &discordgo.User{ID: "user-1", Username: "Yuyuri"}

	targetType, targetID, displayName := subscriptionTargetFromMessage(&discordgo.MessageCreate{Message: &discordgo.Message{
		ChannelID: "dm-channel",
		Author:    user,
	}})
	if targetType != subscriptionTargetUser || targetID != "user-1" || displayName != "Yuyuri" {
		t.Fatalf("dm target = %s %s %q, want user user-1 Yuyuri", targetType, targetID, displayName)
	}

	targetType, targetID, displayName = subscriptionTargetFromMessage(&discordgo.MessageCreate{Message: &discordgo.Message{
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Author:    user,
	}})
	if targetType != subscriptionTargetChannel || targetID != "channel-1" || displayName != "channel:channel-1" {
		t.Fatalf("channel target = %s %s %q, want channel channel-1 channel:channel-1", targetType, targetID, displayName)
	}
}
