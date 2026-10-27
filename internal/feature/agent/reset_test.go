package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestNewResetsOnlyItsConversationAndSkipsOldHistory(t *testing.T) {
	h, d, e := testHandler(t)
	d.channels = map[string]*discordgo.Channel{
		"thread": {ID: "thread", Type: discordgo.ChannelTypeGuildPublicThread, ParentID: "parent", GuildID: "guild"},
	}
	h.state.Threads["thread"] = Conversation{GuildID: "guild", SessionID: "old-thread-session", ContextMessageID: "90"}
	h.state.Threads["dm-other"] = Conversation{SessionID: "other-user-session"}
	h.handle(d, "bot", event("100", "thread", "guild", "<@bot> /new", true))
	// A replay must not enqueue a second reset.
	h.handle(d, "bot", event("100", "thread", "guild", "<@bot> /new", true))
	d.history = []*discordgo.Message{
		{ID: "102", Content: "new discussion", Author: &discordgo.User{ID: "human"}},
		{ID: "100", Content: "<@bot> /new", Author: &discordgo.User{ID: "human"}},
		{ID: "99", Content: "old secret topic", Author: &discordgo.User{ID: "human"}},
	}
	h.handle(d, "bot", event("104", "thread", "guild", "<@bot> fresh question", true))
	if len(h.state.Requests) != 2 || !h.state.Requests[0].Reset {
		t.Fatal("/new must enter the queue exactly once as a reset")
	}
	runWorker(t, h)
	awaitStatus(t, h, "104", "completed")
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.created != 1 || len(e.texts) != 1 || e.sessions[0] == "old-thread-session" {
		t.Fatalf("reset invoked inference or reused old session: created=%d sessions=%v", e.created, e.sessions)
	}
	if strings.Contains(e.texts[0], "old secret topic") || strings.Contains(e.texts[0], "/new") || !strings.Contains(e.texts[0], "new discussion") {
		t.Fatalf("reset boundary lost: %q", e.texts[0])
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.Threads["dm-other"].SessionID != "other-user-session" || h.state.Threads["thread"].ContextMessageID != "104" {
		t.Fatal("reset affected another conversation or rolled back the context cursor")
	}
}

func TestNewIsOrderedAfterRunningRequest(t *testing.T) {
	h, d, e := testHandler(t)
	e.started = make(chan string, 2)
	e.release = make(chan struct{}, 2)
	h.handle(d, "bot", event("10", "dm", "", "<@bot> first", true))
	runWorker(t, h)
	select {
	case <-e.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request never started")
	}
	h.handle(d, "bot", event("11", "dm", "", "<@bot> /new", true))
	h.handle(d, "bot", event("12", "dm", "", "<@bot> second", true))
	e.release <- struct{}{}
	select {
	case id := <-e.started:
		if id != "12" {
			t.Fatalf("reset was sent to the model: %s", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second request never started")
	}
	e.release <- struct{}{}
	awaitStatus(t, h, "12", "completed")
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.created != 2 || len(e.sessions) != 2 || e.sessions[0] == e.sessions[1] {
		t.Fatalf("running request and fresh request shared a session: %v", e.sessions)
	}
}

func TestNewRequiresMentionAndStartsThreadFromChannel(t *testing.T) {
	h, d, _ := testHandler(t)
	h.handle(d, "bot", event("1", "channel", "guild", "/new", false))
	h.handle(d, "bot", event("2", "dm", "", "/new", false))
	if len(h.state.Requests) != 0 {
		t.Fatal("untagged reset was accepted")
	}
	h.handle(d, "bot", event("3", "channel", "guild", "<@bot> /new", true))
	if d.threads != 1 || len(h.state.Requests) != 1 || h.state.Requests[0].ThreadID != "thread-3" || !h.state.Requests[0].Reset {
		t.Fatal("channel /new must create its own fresh thread")
	}
}
