package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/wafflestudio/dicoco/internal/opencode"
)

type fakeModelEngine struct {
	fakeEngine
	selections []string
}

func (e *fakeModelEngine) Models(context.Context) ([]opencode.Model, error) {
	return []opencode.Model{{ID: "big-pickle", ProviderID: "opencode", Name: "Big Pickle"}}, nil
}
func (e *fakeModelEngine) CurrentModel(context.Context) (string, error) {
	return "opencode/big-pickle", nil
}
func (e *fakeModelEngine) SelectModel(_ context.Context, selector string) (opencode.Model, error) {
	e.selections = append(e.selections, selector)
	if selector != "big-pickle" {
		return opencode.Model{}, opencode.ErrModelUnavailable
	}
	return opencode.Model{ID: "big-pickle", ProviderID: "opencode", Name: "Big Pickle"}, nil
}

func TestModelCommandsOnlyInDM(t *testing.T) {
	e := &fakeModelEngine{}
	h := New(e)
	d := &fakeDiscord{}
	// These work without a DM mention and bypass the model inference queue.
	for i, text := range []string{"모델 목록", "모델 현재", "모델 변경 big-pickle", "모델 변경 paid"} {
		h.handle(d, "bot", event(string(rune('a'+i)), "dm", "", text, false))
	}
	if len(e.selections) != 2 || len(d.sent) != 4 || !strings.Contains(d.sent[2], "봇 전체") || !strings.Contains(d.sent[3], "찾지 못") {
		t.Fatalf("DM commands: selections=%v replies=%v", e.selections, d.sent)
	}
	h.handle(d, "bot", event("server", "channel", "guild", "<@bot> 모델 변경 big-pickle", true))
	if len(e.selections) != 2 || !strings.Contains(d.sent[4], "DM에서만") {
		t.Fatal("guild command changed the global model")
	}
	h.handle(d, "bot", event("silent", "channel", "guild", "모델 변경 big-pickle", false))
	if len(d.sent) != 5 {
		t.Fatal("untagged guild command caused a reply")
	}
	if len(h.state.Requests) != 0 || len(h.state.Threads) != 0 || d.threads != 0 {
		t.Fatal("model commands entered inference queue or created conversations")
	}
	if command, _ := modelCommand("모델 성능이 궁금해"); command != "" {
		t.Fatal("ordinary model question was intercepted")
	}
}
