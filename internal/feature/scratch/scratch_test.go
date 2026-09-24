package scratch

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestBlockResultReplies(t *testing.T) {
	for _, status := range []string{"accepted", "unknown", "rejected"} {
		t.Run(status, func(t *testing.T) {
			var reply string
			result := Result{Score: 100, BestHash: [32]byte{0: 0xab}, Submission: status}
			h := &Handler{mine: true, logger: log.New(&bytes.Buffer{}, "", 0),
				run: func(uint64) (Result, error) {
					if status != "accepted" {
						return result, fmt.Errorf("submit failed")
					}
					return result, nil
				},
				send: func(_ *discordgo.Session, _ string, text string, _ *discordgo.MessageReference) (*discordgo.Message, error) {
					reply = text
					return nil, nil
				},
			}
			h.onMessageCreate(nil, &discordgo.MessageCreate{Message: &discordgo.Message{Content: "!긁기", Author: &discordgo.User{ID: "sender"}}})
			if !strings.HasPrefix(reply, "<@sender>\n") || !strings.Contains(reply, "`"+HashString(result.BestHash)+"`") || !strings.Contains(reply, "https://mempool.space/block/"+HashString(result.BestHash)) {
				t.Fatal(reply)
			}
			if strings.Contains(reply, "🎆") != (status == "accepted") || strings.Contains(reply, "✅ 제출이 승인") != (status == "accepted") || strings.Contains(reply, "100점") {
				t.Fatal(reply)
			}
		})
	}
}

func TestScratchCommandRunsTenThousandAttempts(t *testing.T) {
	var output bytes.Buffer
	var reply string
	handler := &Handler{
		logger: log.New(&output, "", 0),
		mine:   true,
		run: func(attempts uint64) (Result, error) {
			if attempts != defaultAttempts {
				t.Fatalf("attempts = %d, want %d", attempts, defaultAttempts)
			}
			return Result{Attempts: attempts, Score: 73, BestHash: [32]byte{31: 0x00, 30: 0x04, 29: 0xa8, 28: 0xc2}, Elapsed: 2 * time.Millisecond, ConnectElapsed: 15 * time.Millisecond}, nil
		},
		send: func(_ *discordgo.Session, _, content string, _ *discordgo.MessageReference) (*discordgo.Message, error) {
			reply = content
			return nil, nil
		},
	}

	handler.onMessageCreate(nil, &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "message-1",
		ChannelID: "channel-1",
		Content:   " !긁기 ",
		Author:    &discordgo.User{ID: "user-1"},
	}})

	if reply != "<@user-1>\n73점 - `0004a8c`" {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if output.Len() != 0 {
		t.Fatalf("unexpected log: %q", output.String())
	}
}

func TestScratchCommandUsesRandomScoreWhenMiningIsDisabled(t *testing.T) {
	var reply string
	handler := &Handler{
		logger: log.Default(),
		mine:   false,
		run: func(uint64) (Result, error) {
			t.Fatal("hashing should be disabled")
			return Result{}, nil
		},
		random: func() (int, error) {
			return 100, nil
		},
		send: func(_ *discordgo.Session, _, content string, _ *discordgo.MessageReference) (*discordgo.Message, error) {
			reply = content
			return nil, nil
		},
	}

	handler.onMessageCreate(nil, &discordgo.MessageCreate{Message: &discordgo.Message{
		ChannelID: "channel-1",
		Content:   "!긁기",
		Author:    &discordgo.User{ID: "user-1"},
	}})

	if reply != "<@user-1>\n100점" {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestScoreBestHashStaysWithinDisplayRange(t *testing.T) {
	var excellent [32]byte
	var poor [32]byte
	for i := range poor {
		poor[i] = 0xff
	}

	if got := scoreBestHash(excellent, defaultAttempts); got != 99 {
		t.Fatalf("excellent score = %d, want 99", got)
	}
	if got := scoreBestHash(poor, defaultAttempts); got != 1 {
		t.Fatalf("poor score = %d, want 1", got)
	}
}

func TestScratchCommandIgnoresGuildMessages(t *testing.T) {
	handler := New()
	handler.run = func(uint64) (Result, error) {
		t.Fatal("guild command should not run")
		return Result{}, nil
	}
	handler.send = func(*discordgo.Session, string, string, *discordgo.MessageReference) (*discordgo.Message, error) {
		t.Fatal("guild command should not reply")
		return nil, nil
	}

	handler.onMessageCreate(nil, &discordgo.MessageCreate{Message: &discordgo.Message{
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Content:   "!긁기",
		Author:    &discordgo.User{ID: "user-1"},
	}})
}

func TestScratchChannelAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name, configured, voice, guild, channel string
		allowed                                 bool
	}{
		{"admin channel", "admin", "voice", "guild", "admin", true},
		{"voice channel", "admin", "voice", "guild", "voice", true},
		{"voice only", "", "voice", "guild", "voice", true},
		{"other channel", "admin", "voice", "guild", "public", false},
		{"missing secret", "", "", "guild", "admin", false},
		{"missing voice secret", "admin", "", "guild", "voice", false},
		{"DM remains open", "admin", "voice", "", "dm", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := &Handler{
				adminChannelID:      tc.configured,
				adminVoiceChannelID: tc.voice,
				logger:              log.Default(),
				random:              func() (int, error) { calls++; return 50, nil },
				send: func(_ *discordgo.Session, channel, text string, ref *discordgo.MessageReference) (*discordgo.Message, error) {
					if !tc.allowed || channel != tc.channel || text != "<@user-1>\n50점" || ref.MessageID != "request" {
						t.Fatalf("unexpected reply: channel=%s text=%s ref=%+v", channel, text, ref)
					}
					return nil, nil
				},
			}
			h.onMessageCreate(nil, &discordgo.MessageCreate{Message: &discordgo.Message{
				ID: "request", GuildID: tc.guild, ChannelID: tc.channel, Content: "!긁기", Author: &discordgo.User{ID: "user-1"},
			}})
			if (calls == 1) != tc.allowed {
				t.Fatalf("run calls=%d, allowed=%v", calls, tc.allowed)
			}
		})
	}
}

func TestScratchMentionTarget(t *testing.T) {
	for _, tc := range []struct{ content, want string }{
		{"!긁기", "sender"},
		{"!긁기 <@123>", "123"},
		{" !긁기  <@!123> ", "123"},
		{"!긁기 @김영희", ""},
		{"!긁기 <@456>", ""},
		{"!긁기 <@123> <@456>", ""},
		{"!긁기 <@&123>", ""},
		{"!긁기 @everyone", ""},
		{"!긁기추가", ""},
	} {
		t.Run(tc.content, func(t *testing.T) {
			calls := 0
			h := &Handler{
				adminChannelID: "admin", logger: log.Default(),
				random: func() (int, error) { calls++; return 50, nil },
				send: func(_ *discordgo.Session, channel, text string, ref *discordgo.MessageReference) (*discordgo.Message, error) {
					if tc.want == "" || text != "<@"+tc.want+">\n50점" || channel != "admin" || ref.MessageID != "request" {
						t.Fatalf("unexpected response: %s %s %+v", channel, text, ref)
					}
					return nil, nil
				},
			}
			h.onMessageCreate(nil, &discordgo.MessageCreate{Message: &discordgo.Message{
				ID: "request", GuildID: "guild", ChannelID: "admin", Content: tc.content,
				Author: &discordgo.User{ID: "sender"}, Mentions: []*discordgo.User{nil, {ID: "123"}},
			}})
			if (calls == 1) != (tc.want != "") {
				t.Fatalf("unexpected run count %d", calls)
			}
		})
	}
}
