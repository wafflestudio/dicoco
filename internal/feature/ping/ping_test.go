package ping

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestMentionsUser(t *testing.T) {
	tests := []struct {
		name     string
		mentions []*discordgo.User
		userID   string
		want     bool
	}{
		{
			name:     "mentioned",
			mentions: []*discordgo.User{{ID: "bot"}},
			userID:   "bot",
			want:     true,
		},
		{
			name:     "different user",
			mentions: []*discordgo.User{{ID: "someone-else"}},
			userID:   "bot",
			want:     false,
		},
		{
			name:     "no mentions",
			mentions: nil,
			userID:   "bot",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mentionsUser(tt.mentions, tt.userID); got != tt.want {
				t.Fatalf("mentionsUser() = %v, want %v", got, tt.want)
			}
		})
	}
}
