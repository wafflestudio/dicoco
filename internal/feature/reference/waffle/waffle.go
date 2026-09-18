package waffle

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

const customEmojiName = "waffle"
const unicodeEmoji = "🧇"

type Handler struct {
	logger      *log.Logger
	now         func() time.Time
	loadMessage func(*discordgo.Session, string, string) (*discordgo.Message, error)
}

func New() *Handler {
	return &Handler{
		logger: log.Default(),
		now:    time.Now,
		loadMessage: func(session *discordgo.Session, channelID, messageID string) (*discordgo.Message, error) {
			if session == nil {
				return nil, fmt.Errorf("Discord session is nil")
			}
			return session.ChannelMessage(channelID, messageID)
		},
	}
}

func (h *Handler) onReactionAdd(session *discordgo.Session, reaction *discordgo.MessageReactionAdd) {
	if reaction == nil || reaction.MessageReaction == nil || !isWaffleEmoji(reaction.Emoji) {
		return
	}

	message, err := h.loadMessage(session, reaction.ChannelID, reaction.MessageID)
	if err != nil {
		h.logger.Printf("load waffle reaction message channel_id=%s message_id=%s: %v", reaction.ChannelID, reaction.MessageID, err)
		return
	}
	if message == nil || message.Author == nil {
		h.logger.Printf("waffle reaction message has no author channel_id=%s message_id=%s", reaction.ChannelID, reaction.MessageID)
		return
	}

	h.logger.Printf(
		"waffle received_at=%s recipient_id=%s recipient_username=%q guild_id=%s channel_id=%s message_id=%s emoji_id=%s",
		h.now().UTC().Format(time.RFC3339),
		message.Author.ID,
		message.Author.Username,
		reaction.GuildID,
		reaction.ChannelID,
		reaction.MessageID,
		reaction.Emoji.ID,
	)
}

func isWaffleEmoji(emoji discordgo.Emoji) bool {
	return emoji.Name == customEmojiName || emoji.Name == unicodeEmoji
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onReactionAdd)
}
