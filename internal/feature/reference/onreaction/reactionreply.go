package onreaction

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

const targetMessageID = "1547403589335384224"
const targetEmoji = "✅"
const reply = "체크 확인"
const errorReply = "체크 처리 실패"

type Handler struct{}

func New() *Handler {
	return &Handler{}
}

func (h *Handler) onReactionAdd(session *discordgo.Session, reaction *discordgo.MessageReactionAdd) {
	if reaction.MessageID != targetMessageID { //맞는 메시지인지
		return
	}
	if reaction.Emoji.Name != targetEmoji { //체크 이모지? 이모티콘?
		return
	}

	if err := session.MessageReactionRemove(
		reaction.ChannelID,
		reaction.MessageID,
		reaction.Emoji.APIName(),
		reaction.UserID,
	); err != nil {
		log.Printf("remove reaction: %v", err)

		if _, err := session.ChannelMessageSend(
			reaction.ChannelID,
			"<@"+reaction.UserID+"> "+errorReply,
		); err != nil {
			log.Printf("send error reply: %v", err)
		}

		return
	}

	if _, err := session.ChannelMessageSend(
		reaction.ChannelID,
		"<@"+reaction.UserID+"> "+reply,
	); err != nil {
		log.Printf("send reply: %v", err)
	}
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onReactionAdd)
}
