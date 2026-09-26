package draw

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/wafflestudio/dicoco/internal/feature/scratch"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		content   string
		n, extras int
		valid     bool
	}{
		{"!사다리 3", 3, 0, true},
		{"!사다리 <@1> <@!2> 2", 2, 2, true},
		{"!사다리", 0, 0, false},
		{"!사다리 0", 0, 0, false},
		{"!사다리 -1", 0, 0, false},
		{"!사다리 1.5", 0, 0, false},
		{"!사다리 <@1> 1", 1, 1, true},
		{"!사다리 2", 2, 0, true},
		{"!사다리 김철수 1", 0, 0, false},
		{"!사다리 <@3> 1", 0, 0, false},
		{"!사다리 <@1> 999999999999999999999999", 0, 0, false},
	} {
		t.Run(tc.content, func(t *testing.T) {
			n, extra, _, err := parse(&discordgo.Message{Content: tc.content, Mentions: []*discordgo.User{{ID: "1"}, {ID: "2"}}})
			if (err == nil) != tc.valid || (tc.valid && (n != tc.n || len(extra) != tc.extras)) {
				t.Fatalf("got %d, %v, %v", n, extra, err)
			}
		})
	}
}

func TestCandidatesMergeAndExcludeBots(t *testing.T) {
	got := candidates([]*discordgo.User{{ID: "1"}, {ID: "2"}, {ID: "bot", Bot: true}}, []*discordgo.User{{ID: "2"}, {ID: "3"}, {ID: "3"}, {ID: "bot2", Bot: true}, nil}, nil)
	if !reflect.DeepEqual(got, []string{"1", "2", "3"}) {
		t.Fatal(got)
	}
}

func TestExclusions(t *testing.T) {
	for _, content := range []string{
		"!사다리 <@3> -<@!2> 1",
		"!사다리 <@2> <@3> -<@2> -<@2> 1",
		"!사다리 -<@2> <@2> <@3> 1",
	} {
		t.Run(content, func(t *testing.T) {
			n, extra, excluded, err := parse(&discordgo.Message{Content: content, Mentions: []*discordgo.User{{ID: "2"}, {ID: "3"}}})
			if err != nil || n != 1 || !reflect.DeepEqual(excluded, map[string]bool{"2": true}) {
				t.Fatalf("got %d, %v, %v", n, excluded, err)
			}
			got := candidates([]*discordgo.User{{ID: "1"}, {ID: "2"}}, extra, excluded)
			if !reflect.DeepEqual(got, []string{"1", "3"}) {
				t.Fatal(got)
			}
		})
	}
	n, extra, excluded, err := parse(&discordgo.Message{Content: "!사다리 -<@1> 1", Mentions: []*discordgo.User{{ID: "1"}}})
	if err != nil || n != 1 || len(extra) != 0 || len(candidates([]*discordgo.User{{ID: "1"}}, extra, excluded)) != 0 {
		t.Fatalf("excluding all candidates: %d, %v, %v, %v", n, extra, excluded, err)
	}
	for _, content := range []string{"!사다리 - <@1> 1", "!사다리 --<@1> 1", "!사다리 -김철수 1", "!사다리 -<@9> 1"} {
		if _, _, _, err := parse(&discordgo.Message{Content: content, Mentions: []*discordgo.User{{ID: "1"}}}); err == nil {
			t.Fatalf("accepted malformed exclusion: %s", content)
		}
	}
}

func TestPickRanksEveryCandidate(t *testing.T) {
	ids := []string{"1", "2", "3", "4"}
	results := []scratch.Result{
		{Score: 20, BestHash: [32]byte{31: 9}},
		{Score: 90, BestHash: [32]byte{0: 1, 31: 2}},
		{Score: 90, BestHash: [32]byte{0: 9, 31: 1}},
		{Score: 50, BestHash: [32]byte{31: 5}},
	}
	for _, n := range []int{1, 3, 4} {
		calls := 0
		h := &Handler{run: func() (scratch.Result, error) {
			result := results[calls]
			calls++
			return result, nil
		}}
		got, err := h.pick(ids, n, nil)
		if err != nil || len(got) != len(ids) || calls != len(ids) {
			t.Fatalf("got %v, %v", got, err)
		}
		want := []string{"3", "2", "4", "1"}
		for i, winner := range got {
			if winner.id != want[i] {
				t.Fatal(got)
			}
		}
	}
	if !reflect.DeepEqual(ids, []string{"1", "2", "3", "4"}) {
		t.Fatal("input changed", ids)
	}
	for _, n := range []int{-1, 0, 5} {
		if _, err := (&Handler{}).pick(ids, n, nil); err == nil {
			t.Fatalf("accepted invalid count %d", n)
		}
	}
}

func TestPickStopsOnMiningFailure(t *testing.T) {
	calls := 0
	h := &Handler{run: func() (scratch.Result, error) {
		calls++
		if calls == 2 {
			return scratch.Result{}, fmt.Errorf("pool unavailable")
		}
		return scratch.Result{Score: 40}, nil
	}}
	got, err := h.pick([]string{"1", "2", "3"}, 1, nil)
	if err == nil || got != nil || calls != 2 {
		t.Fatalf("partial result returned: %v, %v, calls=%d", got, err, calls)
	}
}

func TestPickPreservesBlockNotifications(t *testing.T) {
	for _, status := range []string{"accepted", "rejected", "unknown"} {
		t.Run(status, func(t *testing.T) {
			calls, notices := 0, 0
			h := &Handler{run: func() (scratch.Result, error) {
				calls++
				if calls == 1 {
					result := scratch.Result{Score: 100, Submission: status}
					if status != "accepted" {
						return result, fmt.Errorf("submission failed")
					}
					return result, nil
				}
				return scratch.Result{Score: 99}, nil
			}}
			got, err := h.pick([]string{"1", "2"}, 1, func(id string, result scratch.Result) {
				notices++
				if id != "1" || result.Submission != status || result.BlockMessage() == "" {
					t.Fatalf("wrong block notice: %s, %+v", id, result)
				}
			})
			if err != nil || calls != 2 || notices != 1 || len(got) != 2 || got[0].id != "1" || got[0].result.Score != 100 {
				t.Fatalf("got %v, %v, calls=%d, notices=%d", got, err, calls, notices)
			}
		})
	}
}

func TestVoiceIDsTracksMoves(t *testing.T) {
	state := discordgo.NewState()
	if err := state.GuildAdd(&discordgo.Guild{ID: "g", VoiceStates: []*discordgo.VoiceState{
		{UserID: "1", ChannelID: "voice"}, {UserID: "2", ChannelID: "elsewhere"},
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := voiceIDs(state, "g", "voice")
	if err != nil || !reflect.DeepEqual(got, []string{"1"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	state.OnInterface(&discordgo.Session{StateEnabled: true}, &discordgo.VoiceStateUpdate{VoiceState: &discordgo.VoiceState{GuildID: "g", UserID: "1", ChannelID: "elsewhere"}})
	got, err = voiceIDs(state, "g", "voice")
	if err != nil || len(got) != 0 {
		t.Fatalf("moved user remains: %v, %v", got, err)
	}
	if _, err := voiceIDs(state, "missing", "voice"); err == nil {
		t.Fatal("missing guild accepted")
	}
}

func TestLadderIgnoresTextChannelAndDM(t *testing.T) {
	h := &Handler{voiceChannelID: "voice"}
	for _, message := range []*discordgo.Message{
		{GuildID: "g", ChannelID: "text", Content: "!사다리 3", Author: &discordgo.User{ID: "1"}},
		{ChannelID: "dm", Content: "!사다리 3", Author: &discordgo.User{ID: "1"}},
		{GuildID: "g", ChannelID: "text", Content: "!사다리 <@1> 1", Author: &discordgo.User{ID: "1"}},
		{GuildID: "g", ChannelID: "other", Content: "!사다리 <@1> 1", Author: &discordgo.User{ID: "1"}},
	} {
		// A nil session ensures ignored messages never query Discord or send replies.
		h.onMessageCreate(nil, &discordgo.MessageCreate{Message: message})
	}
}
