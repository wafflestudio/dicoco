package ladder

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/wafflestudio/dicoco/internal/feature/scratch"
)

func TestRoleMentionAndOfflineCandidates(t *testing.T) {
	for _, fields := range [][]string{
		{"!사다리"},
		{"!사다리", "admin"},
		{"!사다리", "<@&123>", "2"},
		{"!사다리", "<@&abc>"},
		{"!박스", "<@&123>"},
	} {
		if _, ok := roleMention(fields); ok {
			t.Fatalf("accepted invalid command: %v", fields)
		}
	}
	if id, ok := roleMention([]string{"!사다리", "<@&123>"}); !ok || id != "123" {
		t.Fatalf("role mention parsed as %q, %v", id, ok)
	}

	members := []*discordgo.Member{
		{User: &discordgo.User{ID: "1", Username: "admin"}, Roles: []string{"123"}},
		{User: &discordgo.User{ID: "2", Username: "other"}, Roles: []string{"456"}},
		{User: &discordgo.User{ID: "3", Username: "bot", Bot: true}, Roles: []string{"123"}},
	}
	candidates := candidatesWithRole(members, "123")
	if len(candidates) != 1 || candidates[0].id != "1" {
		t.Fatalf("role candidates: %+v", candidates)
	}
}

func TestSelectionAcrossPages(t *testing.T) {
	now := time.Now()
	people := make([]candidate, 27)
	for i := range people {
		people[i] = candidate{id: fmt.Sprintf("id-%d", i), label: fmt.Sprintf("사람 %d", i)}
	}
	h := &Handler{
		boxes: map[string]boxState{"box": {
			ownerID: "owner", messageID: "box", candidates: people, created: now,
		}},
		now: func() time.Time { return now },
	}
	if _, message := h.setExcluded("box", "other", 0, []string{"id-0"}); message == "" {
		t.Fatal("another user changed the box")
	}
	if _, message := h.setExcluded("box", "owner", 0, []string{"id-0"}); message != "" {
		t.Fatal(message)
	}
	if _, message := h.movePage("box", "owner", 1); message != "" {
		t.Fatal(message)
	}
	state, message := h.setExcluded("box", "owner", 1, []string{"id-25"})
	if message != "" || len(state.excluded) != 2 || !contains(state.excluded, "id-0") || !contains(state.excluded, "id-25") {
		t.Fatalf("exclusions did not survive pagination: %+v, %s", state.excluded, message)
	}
	if _, message := h.setNumber("box", "owner", 26); message != "" {
		t.Fatal(message)
	}
	if _, message := h.begin("box", "owner"); message == "" {
		t.Fatal("selected more people than remain")
	}
	if _, message := h.setNumber("box", "owner", 2); message != "" {
		t.Fatal(message)
	}
	state, message = h.begin("box", "owner")
	if message != "" || !state.running {
		t.Fatalf("draw did not start: %+v, %s", state, message)
	}
	if _, message := h.begin("box", "owner"); message == "" {
		t.Fatal("duplicate draw started")
	}
}

func TestRankAndResult(t *testing.T) {
	results := []scratch.Result{
		{Score: 90, BestHash: [32]byte{31: 9}},
		{Score: 90, BestHash: [32]byte{31: 2}},
		{Score: 50, BestHash: [32]byte{31: 5}},
	}
	calls := 0
	h := &Handler{run: func() (scratch.Result, error) {
		result := results[calls]
		calls++
		return result, nil
	}}
	ranked, err := h.rank([]candidate{{id: "1"}, {id: "2"}, {id: "3"}}, nil)
	if err != nil || calls != 3 || len(ranked) != 3 {
		t.Fatalf("ranked=%+v, calls=%d, err=%v", ranked, calls, err)
	}
	for i, id := range []string{"2", "1", "3"} {
		if ranked[i].candidate.id != id {
			t.Fatalf("rank %d: got %s, want %s", i+1, ranked[i].candidate.id, id)
		}
	}
	message := strings.Join(resultMessages(ranked, 2, []string{"4"}), "\n")
	for _, want := range []string{"후보 3명 중 2명 선정", "1. <@2> — 90점 - `0200000`", "2. <@1> — 90점 - `0900000`", "나머지 참가자", "<@4>", "동점은 해시값"} {
		if !strings.Contains(message, want) {
			t.Fatalf("result %q does not contain %q", message, want)
		}
	}
}

func TestBlockNoticeSurvivesLaterMiningFailure(t *testing.T) {
	calls, notices := 0, 0
	h := &Handler{run: func() (scratch.Result, error) {
		calls++
		if calls == 1 {
			return scratch.Result{Score: 100, Submission: "accepted"}, nil
		}
		return scratch.Result{}, errors.New("pool unavailable")
	}}
	_, err := h.rank([]candidate{{id: "1"}, {id: "2"}}, func(block rankedUser) {
		notices++
		if block.candidate.id != "1" || block.result.BlockMessage() == "" {
			t.Fatalf("wrong block notice: %+v", block)
		}
	})
	if err == nil || calls != 2 || notices != 1 {
		t.Fatalf("calls=%d, notices=%d, err=%v", calls, notices, err)
	}
}
