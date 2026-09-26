package box

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestRenderAndResult(t *testing.T) {
	state := boxState{
		number:     3,
		candidates: []candidate{{id: "1", label: "사람"}, {id: "2", label: "봇"}},
		excluded:   []string{"2"},
	}
	for _, got := range []string{render(state), result(state)} {
		for _, want := range []string{"숫자: 3", "<@2>"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%q does not contain %q", got, want)
			}
		}
	}
}

func TestComponentsUseFilteredStringOptions(t *testing.T) {
	rows := components(boxState{
		candidates: []candidate{{id: "1", label: "사람"}, {id: "2", label: "봇"}},
		excluded:   []string{"2"},
	})
	row := rows[0].(discordgo.ActionsRow)
	menu := row.Components[0].(discordgo.SelectMenu)
	if menu.MenuType != discordgo.StringSelectMenu || menu.MaxValues != 2 || len(menu.Options) != 2 || !menu.Options[1].Default {
		t.Fatalf("unexpected filtered select menu: %#v", menu)
	}
}

func TestModalTextValue(t *testing.T) {
	components := []discordgo.MessageComponent{&discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		&discordgo.TextInput{CustomID: numberID, Value: "17"},
	}}}
	got, ok := modalTextValue(components, numberID)
	if !ok || got != "17" {
		t.Fatalf("modalTextValue() = %q, %v", got, ok)
	}
}
