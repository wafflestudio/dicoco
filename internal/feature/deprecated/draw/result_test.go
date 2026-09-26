package draw

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wafflestudio/dicoco/internal/feature/scratch"
)

func TestResultMessages(t *testing.T) {
	ranked := []rankedUser{
		{id: "1", result: scratch.Result{Score: 95}},
		{id: "2", result: scratch.Result{Score: 87}},
		{id: "3", result: scratch.Result{Score: 72}},
	}
	got := resultMessages(ranked, 2, map[string]bool{"4": true})
	want := "🪜 사다리 결과 · 후보 3명 중 2명 선정\n\n🎯 선정\n1. <@1> — 95점\n2. <@2> — 87점\n\n나머지 참가자\n3. <@3> — 72점\n\n제외: <@4>"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q", got)
	}
	got = resultMessages(ranked, 3, nil)
	if strings.Contains(got[0], "나머지 참가자") || strings.Contains(got[0], "제외:") || strings.Contains(got[0], "동점") {
		t.Fatal(got)
	}
	ranked[2].result.Score = 87
	got = resultMessages(ranked, 2, nil)
	if !strings.HasSuffix(got[0], "동점은 해시값으로 순위를 정했어요.") {
		t.Fatal(got)
	}
}

func TestResultMessagesSplitWithoutLosingPeople(t *testing.T) {
	var ranked []rankedUser
	excluded := map[string]bool{}
	for i := 0; i < 150; i++ {
		ranked = append(ranked, rankedUser{id: fmt.Sprintf("%018d", i), result: scratch.Result{Score: 90}})
		excluded[fmt.Sprintf("%018d", i+150)] = true
	}
	messages := resultMessages(ranked, 3, excluded)
	if len(messages) < 2 {
		t.Fatal("expected multiple messages")
	}
	for _, message := range messages {
		if len([]rune(message)) > 1900 {
			t.Fatal("message too long")
		}
	}
	joined := strings.Join(messages, "\n")
	for i := 0; i < 300; i++ {
		if strings.Count(joined, fmt.Sprintf("<@%018d>", i)) != 1 {
			t.Fatalf("missing or repeated person %d", i)
		}
	}
}
