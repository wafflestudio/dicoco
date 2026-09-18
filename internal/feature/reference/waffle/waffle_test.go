package waffle

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestOnReactionAddLogsWaffleReaction(t *testing.T) {
	var output bytes.Buffer
	handler := &Handler{
		logger: log.New(&output, "", 0),
		now: func() time.Time {
			return time.Date(2026, time.September, 18, 12, 34, 56, 0, time.FixedZone("KST", 9*60*60))
		},
		loadMessage: func(_ *discordgo.Session, channelID, messageID string) (*discordgo.Message, error) {
			if channelID != "channel-1" || messageID != "message-1" {
				t.Fatalf("unexpected message lookup: channel=%q message=%q", channelID, messageID)
			}
			return &discordgo.Message{Author: &discordgo.User{ID: "author-1", Username: "author"}}, nil
		},
	}

	handler.onReactionAdd(nil, &discordgo.MessageReactionAdd{
		MessageReaction: &discordgo.MessageReaction{
			UserID:    "user-1",
			MessageID: "message-1",
			ChannelID: "channel-1",
			GuildID:   "guild-1",
			Emoji: discordgo.Emoji{
				ID:   "emoji-1",
				Name: customEmojiName,
			},
		},
	})

	logLine := output.String()
	for _, expected := range []string{
		"received_at=2026-09-18T03:34:56Z",
		"recipient_id=author-1",
		`recipient_username="author"`,
		"guild_id=guild-1",
		"channel_id=channel-1",
		"message_id=message-1",
		"emoji_id=emoji-1",
	} {
		if !strings.Contains(logLine, expected) {
			t.Errorf("log line %q does not contain %q", logLine, expected)
		}
	}
	if strings.Contains(logLine, "user-1") {
		t.Errorf("log line contains the user who added the reaction: %q", logLine)
	}
}

func TestOnReactionAddIgnoresOtherEmoji(t *testing.T) {
	var output bytes.Buffer
	handler := &Handler{
		logger: log.New(&output, "", 0),
		now:    time.Now,
		loadMessage: func(_ *discordgo.Session, _, _ string) (*discordgo.Message, error) {
			t.Fatal("message lookup should not be called")
			return nil, nil
		},
	}

	handler.onReactionAdd(nil, &discordgo.MessageReactionAdd{
		MessageReaction: &discordgo.MessageReaction{
			Emoji: discordgo.Emoji{Name: "thumbsup"},
		},
	})

	if output.Len() != 0 {
		t.Fatalf("unexpected log output: %q", output.String())
	}
}

func TestIsWaffleEmojiAcceptsUnicodeWaffle(t *testing.T) {
	if !isWaffleEmoji(discordgo.Emoji{Name: unicodeEmoji}) {
		t.Fatal("Unicode waffle emoji was not recognized")
	}
}
