package waffle

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestOnReactionAddStoresWithoutLogging(t *testing.T) {
	var output bytes.Buffer
	store := &memoryStateStore{state: State{
		Version:    stateVersion,
		Recipients: map[string]int{},
	}}
	handler := &Handler{
		logger:                log.New(&output, "", 0),
		announcementChannelID: "channel-1",
		now: func() time.Time {
			return time.Date(2026, time.September, 18, 12, 34, 56, 0, time.FixedZone("KST", 9*60*60))
		},
		loadMessage: func(_ *discordgo.Session, channelID, messageID string) (*discordgo.Message, error) {
			if channelID != "channel-1" || messageID != "message-1" {
				t.Fatalf("unexpected message lookup: channel=%q message=%q", channelID, messageID)
			}
			return &discordgo.Message{Author: &discordgo.User{ID: "recipient-1", Username: "private-name"}}, nil
		},
		store: store,
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

	if output.Len() != 0 {
		t.Fatalf("successful reaction must not log: %q", output.String())
	}
	if count := store.state.Recipients["recipient-1"]; count != 1 {
		t.Fatalf("unexpected stored count: %d", count)
	}
}

func TestReactionErrorLogHidesDetails(t *testing.T) {
	var output bytes.Buffer
	h := &Handler{
		logger:                log.New(&output, "", 0),
		announcementChannelID: "private-channel",
		loadMessage: func(_ *discordgo.Session, _, _ string) (*discordgo.Message, error) {
			return nil, errors.New("private URL and response body")
		},
	}
	h.onReactionAdd(nil, &discordgo.MessageReactionAdd{MessageReaction: &discordgo.MessageReaction{
		ChannelID: "private-channel", MessageID: "private-message", Emoji: discordgo.Emoji{Name: customEmojiName},
	}})
	if got := output.String(); got != "[waffle] 오류: 메시지 조회 실패\n" {
		t.Fatalf("unexpected error log: %q", got)
	}
}

type memoryStateStore struct {
	state State
}

func TestReactionsOutsideConfiguredChannelAreIgnored(t *testing.T) {
	handler := &Handler{
		announcementChannelID: "admin-channel",
		loadMessage: func(_ *discordgo.Session, _, _ string) (*discordgo.Message, error) {
			t.Fatal("must not load messages or change counts outside the configured channel")
			return nil, nil
		},
	}
	for _, channelID := range []string{"other-channel", "", "thread-id"} {
		reaction := &discordgo.MessageReaction{
			ChannelID: channelID,
			Emoji:     discordgo.Emoji{Name: customEmojiName},
		}
		handler.onReactionAdd(nil, &discordgo.MessageReactionAdd{MessageReaction: reaction})
		handler.onReactionRemove(nil, &discordgo.MessageReactionRemove{MessageReaction: reaction})
	}
}

func TestReactionAddRemoveCycle(t *testing.T) {
	store := &memoryStateStore{state: State{
		Version:    stateVersion,
		Recipients: map[string]int{"recipient": 2},
	}}
	handler := &Handler{
		logger:                log.New(&bytes.Buffer{}, "", 0),
		now:                   time.Now,
		store:                 store,
		announcementChannelID: "channel-1",
		loadMessage: func(_ *discordgo.Session, _, _ string) (*discordgo.Message, error) {
			return &discordgo.Message{Author: &discordgo.User{ID: "recipient"}}, nil
		},
	}
	reaction := &discordgo.MessageReaction{ChannelID: "channel-1", Emoji: discordgo.Emoji{Name: customEmojiName}}
	for range 3 {
		handler.onReactionAdd(nil, &discordgo.MessageReactionAdd{MessageReaction: reaction})
		handler.onReactionRemove(nil, &discordgo.MessageReactionRemove{MessageReaction: reaction})
		if got := store.state.Recipients["recipient"]; got != 2 {
			t.Fatalf("add/remove changed starting count: got %d, want 2", got)
		}
	}
	for range 3 {
		handler.onReactionRemove(nil, &discordgo.MessageReactionRemove{MessageReaction: reaction})
	}
	if _, exists := store.state.Recipients["recipient"]; exists {
		t.Fatal("zero-count recipient should be removed")
	}
	handler.onReactionAdd(nil, &discordgo.MessageReactionAdd{MessageReaction: reaction})
	if got := store.state.Recipients["recipient"]; got != 1 {
		t.Fatalf("re-add count = %d, want 1", got)
	}
}

func TestReactionRemoveIgnoresOtherEmoji(t *testing.T) {
	handler := &Handler{loadMessage: func(_ *discordgo.Session, _, _ string) (*discordgo.Message, error) {
		t.Fatal("unrelated removal must not load a message")
		return nil, nil
	}}
	handler.onReactionRemove(nil, nil)
	handler.onReactionRemove(nil, &discordgo.MessageReactionRemove{})
	handler.onReactionRemove(nil, &discordgo.MessageReactionRemove{
		MessageReaction: &discordgo.MessageReaction{Emoji: discordgo.Emoji{Name: "other"}},
	})
}

func (s *memoryStateStore) Load(context.Context) (State, error) {
	return s.state, nil
}

func (s *memoryStateStore) Save(_ context.Context, state State) error {
	s.state = state
	return nil
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

func TestAnnounceIfDueInitializesSchedule(t *testing.T) {
	now := time.Date(2026, time.September, 18, 3, 0, 0, 0, time.UTC)
	store := &memoryStateStore{state: State{
		Version:    stateVersion,
		Recipients: map[string]int{},
	}}
	handler := &Handler{
		logger: log.New(&bytes.Buffer{}, "", 0),
		now:    func() time.Time { return now },
		store:  store,
		sendMessage: func(_, _ string) error {
			t.Fatal("message should not be sent while initializing the schedule")
			return nil
		},
	}

	if err := handler.announceIfDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.September, 23, 1, 0, 0, 0, time.UTC)
	if store.state.NextAnnouncementAt == nil || !store.state.NextAnnouncementAt.Equal(want) {
		t.Fatalf("unexpected next announcement time: %v", store.state.NextAnnouncementAt)
	}
}

func TestAnnounceIfDueSendsTopThreeAndResetsCounts(t *testing.T) {
	now := time.Date(2026, time.September, 23, 1, 1, 0, 0, time.UTC)
	due := now.Add(-time.Minute)
	store := &memoryStateStore{state: State{
		Version:            stateVersion,
		NextAnnouncementAt: &due,
		Recipients: map[string]int{
			"user-1": 2,
			"user-2": 7,
			"user-3": 4,
			"user-4": 1,
		},
	}}
	var sentChannelID string
	var sentMessage string
	handler := &Handler{
		logger:                log.New(&bytes.Buffer{}, "", 0),
		now:                   func() time.Time { return now },
		announcementChannelID: "channel-1",
		store:                 store,
		sendMessage: func(channelID, message string) error {
			sentChannelID = channelID
			sentMessage = message
			return nil
		},
	}

	if err := handler.announceIfDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sentChannelID != "channel-1" {
		t.Fatalf("unexpected channel ID: %q", sentChannelID)
	}
	for _, expected := range []string{
		"이번 집계 와플 랭킹",
		"🥇 <@user-2> — 7개",
		"🥈 <@user-3> — 4개",
		"🥉 <@user-1> — 2개",
	} {
		if !strings.Contains(sentMessage, expected) {
			t.Errorf("message %q does not contain %q", sentMessage, expected)
		}
	}
	if strings.Contains(sentMessage, "user-4") {
		t.Errorf("message contains a user outside the top three: %q", sentMessage)
	}
	if len(store.state.Recipients) != 0 {
		t.Fatalf("recipients were not reset: %+v", store.state.Recipients)
	}
	if store.state.NextAnnouncementAt == nil || !store.state.NextAnnouncementAt.After(now) {
		t.Fatalf("next announcement time was not advanced: %v", store.state.NextAnnouncementAt)
	}
}
