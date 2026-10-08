package tada

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

const (
	sourceEmojiName = "tada"
	unicodeTada     = "🎉"
)

var targetEmojis = [...]discordgo.Emoji{
	{Name: "tadag", ID: "1557781967947440199"},
	{Name: "acongablob", ID: "1498234383931539456"},
}

type Handler struct{}

func New() *Handler {
	return &Handler{}
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onReactionAdd)
}

func (h *Handler) onReactionAdd(session *discordgo.Session, event *discordgo.MessageReactionAdd) {
	if event == nil || event.MessageReaction == nil || event.GuildID == "" ||
		!isTada(event.Emoji) {
		return
	}
	if session.State != nil && session.State.User != nil && event.UserID == session.State.User.ID {
		return
	}

	for _, target := range targetEmojis {
		if err := session.MessageReactionAdd(event.ChannelID, event.MessageID, target.APIName()); err != nil {
			log.Printf("[tada] add %s reaction: %v", target.Name, err)
		}
	}
}

func isTada(emoji discordgo.Emoji) bool {
	return emoji.Name == unicodeTada || (emoji.ID != "" && emoji.Name == sourceEmojiName)
}
