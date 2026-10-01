package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestInChannelOrThread(t *testing.T) {
	state := discordgo.NewState()
	if err := state.GuildAdd(&discordgo.Guild{
		ID: "guild",
		Channels: []*discordgo.Channel{
			{ID: "admin", GuildID: "guild", Type: discordgo.ChannelTypeGuildText},
			{ID: "other", GuildID: "guild", Type: discordgo.ChannelTypeGuildText},
		},
		Threads: []*discordgo.Channel{
			{ID: "admin-thread", GuildID: "guild", ParentID: "admin", Type: discordgo.ChannelTypeGuildPublicThread},
			{ID: "other-thread", GuildID: "guild", ParentID: "other", Type: discordgo.ChannelTypeGuildPrivateThread},
		},
	}); err != nil {
		t.Fatal(err)
	}
	session := &discordgo.Session{State: state}
	for _, tc := range []struct {
		channel string
		allowed bool
	}{
		{"admin", true},
		{"admin-thread", true},
		{"other", false},
		{"other-thread", false},
	} {
		if got := InChannelOrThread(session, tc.channel, "admin"); got != tc.allowed {
			t.Fatalf("channel %s: got %t, want %t", tc.channel, got, tc.allowed)
		}
	}
	if InChannelOrThread(nil, "admin-thread", "admin") {
		t.Fatal("thread accepted without session data")
	}
}
