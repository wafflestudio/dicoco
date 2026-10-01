package discord

import "github.com/bwmarrin/discordgo"

// InChannelOrThread reports whether channelID is allowed or is a thread of an allowed channel.
func InChannelOrThread(session *discordgo.Session, channelID string, allowedIDs ...string) bool {
	for _, id := range allowedIDs {
		if id != "" && channelID == id {
			return true
		}
	}
	if session == nil {
		return false
	}
	var channel *discordgo.Channel
	if session.State != nil {
		channel, _ = session.State.Channel(channelID)
	}
	if channel == nil {
		var err error
		channel, err = session.Channel(channelID)
		if err != nil {
			return false
		}
	}
	if !channel.IsThread() {
		return false
	}
	for _, id := range allowedIDs {
		if id != "" && channel.ParentID == id {
			return true
		}
	}
	return false
}
