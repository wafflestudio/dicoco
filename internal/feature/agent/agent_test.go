package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/bwmarrin/discordgo"
)

type fakeDiscord struct {
	mu       sync.Mutex
	threads  int
	sent     []string
	channels map[string]*discordgo.Channel
	history  []*discordgo.Message
	root     *discordgo.Message
}

func (d *fakeDiscord) Channel(id string, opts ...discordgo.RequestOption) (*discordgo.Channel, error) {
	if c := d.channels[id]; c != nil {
		return c, nil
	}
	return &discordgo.Channel{ID: id, Type: discordgo.ChannelTypeGuildText, GuildID: "guild"}, nil
}
func (d *fakeDiscord) ChannelMessages(id string, limit int, before, after, around string, opts ...discordgo.RequestOption) ([]*discordgo.Message, error) {
	return d.history, nil
}
func (d *fakeDiscord) ChannelMessage(channel, id string, opts ...discordgo.RequestOption) (*discordgo.Message, error) {
	return d.root, nil
}

func (d *fakeDiscord) MessageThreadStart(channel, id, name string, duration int, opts ...discordgo.RequestOption) (*discordgo.Channel, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.threads++
	return &discordgo.Channel{ID: "thread-" + id}, nil
}
func (d *fakeDiscord) ChannelMessageSendComplex(channel string, m *discordgo.MessageSend, opts ...discordgo.RequestOption) (*discordgo.Message, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if m.AllowedMentions == nil || len(m.AllowedMentions.Parse) != 0 {
		return nil, errors.New("bot responses must not ping users")
	}
	d.sent = append(d.sent, channel+":"+m.Content)
	return &discordgo.Message{}, nil
}

type fakeEngine struct {
	mu                   sync.Mutex
	created, interrupted int
	ids, sessions, texts []string
	started              chan string
	release              chan struct{}
}

func (e *fakeEngine) Create(ctx context.Context, title string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.created++
	return fmt.Sprintf("ses_%d", e.created), nil
}
func (e *fakeEngine) Ask(ctx context.Context, session, id, text string) (string, error) {
	e.mu.Lock()
	e.ids = append(e.ids, id)
	e.sessions = append(e.sessions, session)
	e.texts = append(e.texts, text)
	e.mu.Unlock()
	if e.started != nil {
		e.started <- id
	}
	if e.release != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-e.release:
		}
	}
	return "answer " + id, nil
}
func (e *fakeEngine) Interrupt(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.interrupted++
	return nil
}

func testHandler(t *testing.T) (*Handler, *fakeDiscord, *fakeEngine) {
	t.Helper()
	e := &fakeEngine{}
	d := &fakeDiscord{}
	h := New("channel", e)
	h.discord = d
	return h, d, e
}
func event(id, channel, guild, text string, mention bool) *discordgo.MessageCreate {
	m := &discordgo.MessageCreate{Message: &discordgo.Message{ID: id, ChannelID: channel, GuildID: guild, Content: text, Author: &discordgo.User{ID: "author", Username: "지훈"}}}
	if mention {
		m.Mentions = []*discordgo.User{{ID: "bot"}}
	}
	return m
}
func runWorker(t *testing.T, h *Handler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); h.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("worker did not stop")
		}
	})
}
func awaitStatus(t *testing.T, h *Handler, id, status string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		found := false
		for _, r := range h.state.Requests {
			if r.ID == id && r.Status == status {
				found = true
			}
		}
		h.mu.Unlock()
		if found {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("request %s did not reach %s", id, status)
}

func TestMentionsRoutingAndDedup(t *testing.T) {
	h, d, _ := testHandler(t)
	h.handle(d, "bot", nil)
	h.handle(d, "bot", event("1", "other", "guild", "<@bot> ignored", true))
	h.handle(d, "bot", event("2", "channel", "guild", "ordinary chat", false))
	m := event("3", "channel", "guild", "<@bot> bot message", true)
	m.Author.Bot = true
	h.handle(d, "bot", m)
	h.handle(d, "bot", event("4", "untracked-thread", "guild", "<@bot> ignored", true))
	request := event("5", "channel", "guild", "<@bot> explain code", true)
	h.handle(d, "bot", request)
	h.handle(d, "bot", request)
	h.handle(d, "bot", event("ignored-chat", "thread-5", "guild", "ordinary followup chat", false))
	h.handle(d, "bot", event("6", "thread-5", "guild", "<@bot> also check tests", true))
	if d.threads != 1 || len(h.state.Requests) != 2 {
		t.Fatalf("threads=%d requests=%d", d.threads, len(h.state.Requests))
	}
	if h.state.Requests[0].Text != "explain code" || h.state.Requests[1].AuthorID != "author" {
		t.Fatal("lost request text or identity")
	}
}

func TestDMWithoutGuildChannelConfigured(t *testing.T) {
	_, d, e := testHandler(t)
	h := New("", e)
	h.discord = d
	h.handle(d, "bot", event("guild", "channel", "guild", "<@bot> ignored without admin", true))
	h.handle(d, "bot", event("dm", "dm-channel", "", "question without mention", false))
	if len(h.state.Requests) != 1 || h.state.Requests[0].ID != "dm" {
		t.Fatal("DM must work without an admin channel; guild messages must stay disabled")
	}
	runWorker(t, h)
	awaitStatus(t, h, "dm", "completed")
}

func TestSequentialFollowupsAndDMSessions(t *testing.T) {
	h, d, e := testHandler(t)
	e.started = make(chan string, 4)
	e.release = make(chan struct{}, 4)
	h.handle(d, "bot", event("first", "channel", "guild", "<@bot> question", true))
	h.handle(d, "bot", event("followup", "thread-first", "guild", "<@bot> follow up", true))
	h.handle(d, "bot", event("dm", "dm-channel", "", "private question", false))
	h.handle(d, "bot", event("other", "channel", "guild", "<@bot> separate thread", true))
	runWorker(t, h)
	for _, id := range []string{"first", "followup", "dm", "other"} {
		select {
		case got := <-e.started:
			if got != id {
				t.Fatalf("started %s, want %s", got, id)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no work")
		}
		select {
		case got := <-e.started:
			t.Fatalf("overlapping analysis %s", got)
		default:
		}
		e.release <- struct{}{}
		awaitStatus(t, h, id, "completed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.created != 3 || e.sessions[0] != e.sessions[1] || e.sessions[0] == e.sessions[2] || e.sessions[0] == e.sessions[3] || e.sessions[2] == e.sessions[3] {
		t.Fatalf("session isolation: %v", e.sessions)
	}
	if d.threads != 2 {
		t.Fatal("expected two server threads; DM must not create a thread")
	}
	if !strings.Contains(e.texts[0], "author") {
		t.Fatal("missing author")
	}
}

func TestMentionInExistingThreadAndFreshStart(t *testing.T) {
	h, d, e := testHandler(t)
	d.channels = map[string]*discordgo.Channel{
		"existing": {ID: "existing", Type: discordgo.ChannelTypeGuildPublicThread, ParentID: "channel", GuildID: "guild"},
		"foreign":  {ID: "foreign", Type: discordgo.ChannelTypeGuildPublicThread, ParentID: "other", GuildID: "guild"},
	}
	d.history = []*discordgo.Message{{Content: "prior detail", Author: &discordgo.User{ID: "teammate", Username: "민수"}}}
	d.root = &discordgo.Message{ID: "existing", Content: "original PR link", Author: &discordgo.User{ID: "rootauthor"}}
	h.handle(d, "bot", event("ignored", "foreign", "guild", "<@bot> ignored", true))
	h.handle(d, "bot", event("initial", "existing", "guild", "<@bot> investigate", true))
	h.handle(d, "bot", event("followup", "existing", "guild", "<@bot> check tests too", true))
	if len(h.state.Requests) != 2 || d.threads != 0 {
		t.Fatal("existing thread should be reused only under configured parent")
	}
	if !strings.Contains(h.state.Requests[0].Context, "prior detail") || !strings.Contains(h.state.Requests[0].Context, "original PR link") || h.state.Requests[1].Context != "" {
		t.Fatal("expected history only on initial thread activation")
	}
	runWorker(t, h)
	awaitStatus(t, h, "followup", "completed")
	fresh := New("channel", e)
	if len(fresh.state.Threads) != 0 || len(fresh.state.Requests) != 0 {
		t.Fatal("restart must start fresh without disk persistence")
	}
}

func TestOnlyNewHumanContextOnSubsequentMention(t *testing.T) {
	h, d, _ := testHandler(t)
	d.channels = map[string]*discordgo.Channel{"existing": {ID: "existing", Type: discordgo.ChannelTypeGuildPublicThread, ParentID: "channel", GuildID: "guild"}}
	d.history = []*discordgo.Message{{ID: "99", Content: "old discussion", Author: &discordgo.User{ID: "human"}}}
	h.handle(d, "bot", event("100", "existing", "guild", "<@bot> first", true))
	d.history = []*discordgo.Message{
		{ID: "103", Content: "previous bot output", Author: &discordgo.User{ID: "bot"}},
		{ID: "102", Content: "new teammate discussion", Author: &discordgo.User{ID: "human"}},
		{ID: "100", Content: "previous request", Author: &discordgo.User{ID: "human"}},
		{ID: "99", Content: "old discussion", Author: &discordgo.User{ID: "human"}},
	}
	h.handle(d, "bot", event("104", "existing", "guild", "<@bot> followup", true))
	context := h.state.Requests[1].Context
	if !strings.Contains(context, "new teammate discussion") || strings.Contains(context, "old discussion") || strings.Contains(context, "previous request") || strings.Contains(context, "previous bot output") {
		t.Fatalf("unexpected context: %s", context)
	}
}

func TestTimeoutInterruptsBeforeNextRequest(t *testing.T) {
	h, d, e := testHandler(t)
	h.timeout = 15 * time.Millisecond
	e.release = make(chan struct{})
	h.handle(d, "bot", event("timeout", "dm", "", "question", false))
	runWorker(t, h)
	awaitStatus(t, h, "timeout", "failed")
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.interrupted != 1 {
		t.Fatal("timed-out server execution was not stopped")
	}
}

func TestSplitMessagePreservesUnicode(t *testing.T) {
	text := strings.Repeat("한글😀", 1500)
	parts := splitMessage(text)
	if strings.Join(parts, "") != text {
		t.Fatal("content lost")
	}
	for _, part := range parts {
		if len(utf16.Encode([]rune(part))) > 1900 {
			t.Fatal("Discord message too long")
		}
	}
}
