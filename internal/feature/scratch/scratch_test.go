package scratch

import (
	"bytes"
	"log"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

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

	if reply != "73점 - `0004a8c`" {
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

	if reply != "100점" {
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
