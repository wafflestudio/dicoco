package ping

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

const defaultReply = "네, 불렀나요?"

type Handler struct {
	reply string
}

func New() *Handler {
	return &Handler{reply: defaultReply}
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onMessageCreate)
}

func (h *Handler) onMessageCreate(session *discordgo.Session, message *discordgo.MessageCreate) {
	if message.Author == nil || message.Author.Bot {
		return
	}
	if session.State == nil || session.State.User == nil {
		return
	}
	if !mentionsUser(message.Mentions, session.State.User.ID) {
		return
	}

	if _, err := session.ChannelMessageSendReply(
		message.ChannelID,
		h.reply,
		message.Reference(),
	); err != nil {
		log.Printf("reply to Discord ping: %v", err)
	}
}

func mentionsUser(mentions []*discordgo.User, userID string) bool {
	for _, user := range mentions {
		if user != nil && user.ID == userID {
			return true
		}
	}
	return false
}
