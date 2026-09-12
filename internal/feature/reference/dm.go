package reference

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

const dmReply = "메시지를 수신했습니다."

type Handler struct{}

func New() *Handler {
	return &Handler{}
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onMessageCreate)
}

func (h *Handler) onMessageCreate(session *discordgo.Session, message *discordgo.MessageCreate) {
	if message.GuildID != "" {
		return
	}
	if message.Author == nil || message.Author.Bot {
		return
	}

	if _, err := session.ChannelMessageSendReply(
		message.ChannelID,
		dmReply,
		message.Reference(),
	); err != nil {
		log.Printf("reply to Discord DM: %v", err)
	}
}
