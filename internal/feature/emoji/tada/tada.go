package tada

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

const sourceEmojiName = "tada"

var targetEmojiNames = [...]string{"tadag", "acongablob"}

type Handler struct{}

func New() *Handler {
	return &Handler{}
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onReactionAdd)
}

func (h *Handler) onReactionAdd(session *discordgo.Session, event *discordgo.MessageReactionAdd) {
	if event == nil || event.MessageReaction == nil || event.GuildID == "" ||
		event.Emoji.ID == "" || event.Emoji.Name != sourceEmojiName {
		return
	}
	if session.State != nil && session.State.User != nil && event.UserID == session.State.User.ID {
		return
	}

	emojis, err := findTargetEmojis(session, event.GuildID)
	if err != nil {
		log.Printf("[tada] find emojis: %v", err)
		return
	}

	for _, name := range targetEmojiNames {
		target := namedEmoji(emojis, name)
		if target == nil {
			log.Printf("[tada] emoji %q not found in guild", name)
			continue
		}
		if err := session.MessageReactionAdd(event.ChannelID, event.MessageID, target.APIName()); err != nil {
			log.Printf("[tada] add %s reaction: %v", name, err)
		}
	}
}

func findTargetEmojis(session *discordgo.Session, guildID string) ([]*discordgo.Emoji, error) {
	if session.State != nil {
		if guild, err := session.State.Guild(guildID); err == nil {
			if allTargetsPresent(guild.Emojis) {
				return guild.Emojis, nil
			}
		}
	}

	return session.GuildEmojis(guildID)
}

func allTargetsPresent(emojis []*discordgo.Emoji) bool {
	for _, name := range targetEmojiNames {
		if namedEmoji(emojis, name) == nil {
			return false
		}
	}
	return true
}

func namedEmoji(emojis []*discordgo.Emoji, name string) *discordgo.Emoji {
	for _, emoji := range emojis {
		if emoji != nil && emoji.ID != "" && emoji.Name == name {
			return emoji
		}
	}
	return nil
}
