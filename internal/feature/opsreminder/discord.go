package opsreminder

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

const doneEmojiID = "1501195973865640040"

type discordReaction struct {
	Count int `json:"count"`
	Emoji struct {
		ID string `json:"id"`
	} `json:"emoji"`
}

type discordMessage struct {
	ID        string            `json:"id"`
	ChannelID string            `json:"channel_id"`
	Content   string            `json:"content"`
	Timestamp string            `json:"timestamp"`
	Reactions []discordReaction `json:"reactions"`
	Author    struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	} `json:"author"`
	Thread *struct {
		ID       string `json:"id"`
		ParentID string `json:"parent_id"`
	} `json:"thread"`
}
type discordChannel struct {
	ID       string `json:"id"`
	GuildID  string `json:"guild_id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
}

func hasDoneReaction(m discordMessage) bool {
	for _, reaction := range m.Reactions {
		if reaction.Count > 0 && reaction.Emoji.ID == doneEmojiID {
			return true
		}
	}
	return false
}

type discordClient struct {
	api        *apiClient
	token      string
	botID      string
	sourceID   string
	reminderID string
	guildID    string
	now        time.Time
}

func (d *discordClient) initialize(ctx context.Context) error {
	var me struct {
		ID string `json:"id"`
	}
	if err := d.api.request(ctx, "GET", "https://discord.com/api/v10/users/@me", discordHeaders(d.token), nil, &me, true); err != nil {
		return err
	}
	if !snowflake(me.ID) {
		return fmt.Errorf("Discord bot identity invalid")
	}
	d.botID = me.ID
	var source, reminder discordChannel
	for _, item := range []struct {
		id  string
		out *discordChannel
	}{{d.sourceID, &source}, {d.reminderID, &reminder}} {
		if err := d.api.request(ctx, "GET", "https://discord.com/api/v10/channels/"+url.PathEscape(item.id), discordHeaders(d.token), nil, item.out, true); err != nil {
			return err
		}
		if item.out.ID != item.id || item.out.GuildID == "" {
			return fmt.Errorf("Discord channel identity invalid")
		}
	}
	if source.GuildID != reminder.GuildID {
		return fmt.Errorf("Discord channels are in different guilds")
	}
	d.guildID = source.GuildID
	return nil
}

func (d discordClient) messages(ctx context.Context, channelID string, since time.Time, maxPages int) ([]discordMessage, error) {
	endpoint := "https://discord.com/api/v10/channels/" + url.PathEscape(channelID) + "/messages?limit=100"
	all := []discordMessage{}
	for page := 0; page < maxPages; page++ {
		var messages []discordMessage
		if err := d.api.request(ctx, "GET", endpoint, discordHeaders(d.token), nil, &messages, true); err != nil {
			return nil, err
		}
		if len(messages) == 0 {
			return all, nil
		}
		oldest := d.now
		for _, m := range messages {
			at, err := time.Parse(time.RFC3339Nano, m.Timestamp)
			if err != nil {
				return nil, fmt.Errorf("Discord message timestamp invalid")
			}
			if at.Before(oldest) {
				oldest = at
			}
			if !at.Before(since) {
				all = append(all, m)
			}
		}
		if oldest.Before(since) || len(messages) < 100 {
			return all, nil
		}
		endpoint = "https://discord.com/api/v10/channels/" + url.PathEscape(channelID) + "/messages?limit=100&before=" + url.QueryEscape(messages[len(messages)-1].ID)
	}
	return nil, fmt.Errorf("Discord message pagination exceeded %d pages", maxPages)
}

func (d discordClient) attachSourceThreads(ctx context.Context, candidates []Candidate) error {
	// A source-channel message links a task only when it contains its exact Notion URL or ID.
	unlinked := false
	for _, c := range candidates {
		if len(c.ThreadIDs) == 0 {
			unlinked = true
			break
		}
	}
	if !unlinked {
		return nil
	}
	messages, err := d.messages(ctx, d.sourceID, d.now.AddDate(0, 0, -30), 20)
	if err != nil {
		return fmt.Errorf("Discord source channel: %w", err)
	}
	for i := range candidates {
		c := &candidates[i]
		if len(c.ThreadIDs) > 0 {
			continue
		}
		for _, m := range messages {
			if m.Thread == nil || m.Thread.ParentID != d.sourceID {
				continue
			}
			if strings.Contains(m.Content, c.URL) || strings.Contains(m.Content, c.ID) || strings.Contains(m.Content, strings.ReplaceAll(c.ID, "-", "")) {
				c.ThreadIDs = append(c.ThreadIDs, m.Thread.ID)
			}
		}
		c.ThreadIDs = uniqueStrings(c.ThreadIDs)
	}
	return nil
}
func (d discordClient) attachThreadEvidence(ctx context.Context, candidates []Candidate) error {
	owners := map[string]string{}
	for _, c := range candidates {
		for _, threadID := range c.ThreadIDs {
			if previous := owners[threadID]; previous != "" && previous != c.ID {
				return fmt.Errorf("Discord thread %s is linked to multiple TODOs", threadID)
			}
			owners[threadID] = c.ID
		}
	}
	for i := range candidates {
		c := &candidates[i]
		if len(c.ThreadIDs) > 3 {
			return fmt.Errorf("task %s links more than 3 threads", c.ID)
		}
		for _, threadID := range c.ThreadIDs {
			if !snowflake(threadID) {
				return fmt.Errorf("invalid thread ID for task %s", c.ID)
			}
			var channel discordChannel
			if err := d.api.request(ctx, "GET", "https://discord.com/api/v10/channels/"+url.PathEscape(threadID), discordHeaders(d.token), nil, &channel, true); err != nil {
				return fmt.Errorf("Discord thread %s: %w", threadID, err)
			}
			if channel.ID != threadID || channel.GuildID != d.guildID {
				return fmt.Errorf("Discord thread identity invalid")
			}
			// A thread created from a channel message has the same ID as its
			// parent message. Forum-style threads can instead start inside the thread.
			if channel.ParentID != "" {
				var starter discordMessage
				endpoint := "https://discord.com/api/v10/channels/" + url.PathEscape(channel.ParentID) + "/messages/" + url.PathEscape(threadID)
				err := d.api.request(ctx, "GET", endpoint, discordHeaders(d.token), nil, &starter, true)
				if err != nil {
					var status *apiHTTPError
					if !errors.As(err, &status) || status.StatusCode != 404 {
						return fmt.Errorf("Discord thread starter %s: %w", threadID, err)
					}
				} else if starter.ID == threadID && hasDoneReaction(starter) {
					c.DoneReaction = true
				}
			}
			// Read the whole linked thread so an older Done decision cannot be missed.
			messages, err := d.messages(ctx, threadID, time.Time{}, 10)
			if err != nil {
				return fmt.Errorf("Discord thread %s: %w", threadID, err)
			}
			sort.Slice(messages, func(i, j int) bool { return messages[i].Timestamp < messages[j].Timestamp })
			for _, m := range messages {
				if hasDoneReaction(m) {
					c.DoneReaction = true
				}
				if m.Content == "" || m.Author.Bot {
					continue
				}
				at, _ := time.Parse(time.RFC3339Nano, m.Timestamp)
				c.Evidence = append(c.Evidence, Evidence{Ref: m.ID, URL: "https://discord.com/channels/" + d.guildID + "/" + threadID + "/" + m.ID, Text: m.Content, At: at, Kind: "discord_thread_message"})
			}
		}
	}
	return nil
}
func (d discordClient) recentReminders(ctx context.Context) ([]discordMessage, error) {
	return d.messages(ctx, d.reminderID, d.now.AddDate(0, 0, -3), 20)
}
func (d discordClient) alreadySent(messages []discordMessage, r Reminder) bool {
	for _, m := range messages {
		if m.Author.ID != d.botID || !strings.HasPrefix(m.Content, "⏰ 운영팀 TODO · 내일 마감") {
			continue
		}
		if strings.Contains(m.Content, r.NotionURL) && strings.Contains(m.Content, "마감: "+r.DueDate+" (KST)") {
			return true
		}
	}
	return false
}

func digestHeading(dueDate string, testMode bool) string {
	if testMode {
		return "🧪 운영팀 TODO 소규모 테스트 · " + dueDate + " 마감 (KST)"
	}
	return "⏰ 운영팀 TODO · " + dueDate + " 마감 (KST)"
}

func (d discordClient) alreadySentDigest(messages []discordMessage, dueDate string, testMode bool) bool {
	for _, m := range messages {
		if m.Author.ID == d.botID && strings.HasPrefix(m.Content, digestHeading(dueDate, testMode)) {
			return true
		}
	}
	return false
}

func safeDiscordText(s string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), "@", "@\u200b")
}

func (d discordClient) formatDigest(dueDate string, reminders []Reminder, uncertain []UncertainItem, testMode bool, processed, collected int) (string, []string, error) {
	content := digestHeading(dueDate, testMode)
	if testMode {
		content += fmt.Sprintf("\n최근 후보 %d/%d개만 검토한 테스트 결과", processed, collected)
	}
	if len(reminders) == 0 && len(uncertain) == 0 {
		content += "\n\n이 샘플에서 내일 마감으로 분류된 TODO 없음"
	}
	mentionIDs := []string{}
	if len(reminders) > 0 {
		content += "\n\n✅ 확정"
	}
	for _, r := range reminders {
		if r.DueDate != dueDate || r.NotionURL == "" || r.Title == "" {
			return "", nil, fmt.Errorf("confirmed TODO is missing due date or source")
		}
		mentions := []string{}
		for _, id := range r.UserIDs {
			if !snowflake(id) {
				return "", nil, fmt.Errorf("invalid confirmed assignee ID")
			}
			mentions = append(mentions, "<@"+id+">")
			mentionIDs = append(mentionIDs, id)
		}
		content += "\n" + strings.Join(mentions, " ") + " " + safeDiscordText(r.Title) + "\n근거: " + r.NotionURL
		if r.ThreadURL != "" {
			content += " · " + r.ThreadURL
		}
	}
	if len(uncertain) > 0 {
		content += "\n\n❔ 확인 필요 · 멘션 없음"
	}
	for _, item := range uncertain {
		if item.DueDate != dueDate || item.NotionURL == "" {
			return "", nil, fmt.Errorf("uncertain TODO is missing due date or source")
		}
		content += "\n" + safeDiscordText(item.Title) + "\n사유: " + safeDiscordText(item.Reason) + "\n근거: " + item.NotionURL
	}
	if len(utf16.Encode([]rune(content))) > 1900 {
		return "", nil, fmt.Errorf("TODO digest exceeds Discord message limit")
	}
	return content, uniqueStrings(mentionIDs), nil
}

func (d discordClient) sendDigest(ctx context.Context, content string, mentionIDs []string) (string, error) {
	var sent struct {
		ID string `json:"id"`
	}
	body := map[string]any{"content": content, "allowed_mentions": map[string]any{"users": mentionIDs}}
	if err := d.api.request(ctx, "POST", "https://discord.com/api/v10/channels/"+url.PathEscape(d.reminderID)+"/messages", discordHeaders(d.token), body, &sent, false); err != nil {
		return "", err
	}
	if !snowflake(sent.ID) {
		return "", fmt.Errorf("Discord returned invalid message ID")
	}
	return sent.ID, nil
}
func (d discordClient) format(r Reminder) (string, error) {
	mentions := make([]string, 0, len(r.UserIDs))
	for _, id := range r.UserIDs {
		if !snowflake(id) {
			return "", fmt.Errorf("invalid Discord user ID")
		}
		mentions = append(mentions, "<@"+id+">")
	}
	title := strings.Join(strings.Fields(r.Title), " ")
	if title == "" || r.NotionURL == "" {
		return "", fmt.Errorf("reminder missing title or source")
	}
	content := "⏰ 운영팀 TODO · 내일 마감\n\n" + strings.Join(mentions, " ") + " " + title + "\n마감: " + r.DueDate + " (KST)\n근거: " + r.NotionURL
	if r.ThreadURL != "" {
		content += " · " + r.ThreadURL
	}
	if len(utf16.Encode([]rune(content))) > 1900 {
		return "", fmt.Errorf("reminder exceeds Discord content limit")
	}
	return content, nil
}
func (d discordClient) send(ctx context.Context, r Reminder) (string, error) {
	content, err := d.format(r)
	if err != nil {
		return "", err
	}
	var sent struct {
		ID string `json:"id"`
	}
	body := map[string]any{"content": content, "allowed_mentions": map[string]any{"users": r.UserIDs}}
	err = d.api.request(ctx, "POST", "https://discord.com/api/v10/channels/"+url.PathEscape(d.reminderID)+"/messages", discordHeaders(d.token), body, &sent, false)
	if err != nil {
		return "", err
	}
	if !snowflake(sent.ID) {
		return "", fmt.Errorf("Discord returned invalid message ID")
	}
	return sent.ID, nil
}
