package dm

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

type receiptTransport struct{ calls int }

func (r *receiptTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls++
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"receipt"}`))}, nil
}

func TestDMReceiptRouting(t *testing.T) {
	for _, tc := range []struct {
		name, content, guild string
		mention              bool
		want                 int
	}{
		{"ordinary DM", "hello", "", false, 1},
		{"implicit reply mention", "hello", "", true, 1},
		{"tagged DM", "<@bot> hello", "", true, 0},
		{"nickname tag", "<@!bot> hello", "", true, 0},
		{"existing command exclusion", "!긁기", "", false, 0},
		{"guild message", "hello", "guild", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := discordgo.New("Bot test")
			if err != nil {
				t.Fatal(err)
			}
			s.State.User = &discordgo.User{ID: "bot"}
			transport := &receiptTransport{}
			s.Client = &http.Client{Transport: transport}
			m := &discordgo.MessageCreate{Message: &discordgo.Message{ID: "message", ChannelID: "dm", GuildID: tc.guild, Content: tc.content, Author: &discordgo.User{ID: "human"}}}
			if tc.mention {
				m.Mentions = []*discordgo.User{{ID: "bot"}}
			}
			New().onMessageCreate(s, m)
			if transport.calls != tc.want {
				t.Fatalf("receipt requests=%d, want %d", transport.calls, tc.want)
			}
		})
	}
}
