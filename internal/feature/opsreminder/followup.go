package opsreminder

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

func previousSaturday(now time.Time) time.Time {
	days := (int(now.Weekday()) + 1) % 7
	if days == 0 {
		days = 7
	}
	return now.AddDate(0, 0, -days)
}

func followupHeading(meetingDate string) string {
	return "📋 운영팀 회의 후속 TODO · " + meetingDate
}

func (d discordClient) alreadySentFollowup(messages []discordMessage, meetingDate string, item MeetingFollowup) bool {
	for _, m := range messages {
		if m.Author.ID == d.botID && strings.HasPrefix(m.Content, followupHeading(meetingDate)) && strings.Contains(m.Content, item.NotionURL) {
			return true
		}
	}
	return false
}

func (d discordClient) formatFollowup(meetingDate string, item MeetingFollowup) (string, error) {
	if item.Title == "" || item.NotionURL == "" {
		return "", fmt.Errorf("meeting followup missing title or source")
	}
	mentions := []string{}
	for _, id := range item.UserIDs {
		if !snowflake(id) {
			return "", fmt.Errorf("meeting followup has invalid assignee ID")
		}
		mentions = append(mentions, "<@"+id+">")
	}
	content := followupHeading(meetingDate) + "\n\n"
	if len(mentions) > 0 {
		content += strings.Join(mentions, " ") + " "
	}
	content += safeDiscordText(item.Title)
	if item.DueDate != "" {
		content += "\n기한: " + item.DueDate + " (KST)"
	} else {
		content += "\n기한: 미정"
	}
	content += "\n근거: " + item.NotionURL
	if item.ThreadURL != "" {
		content += " · " + item.ThreadURL
	}
	if len(utf16.Encode([]rune(content))) > 1900 {
		return "", fmt.Errorf("meeting followup exceeds Discord message limit")
	}
	return content, nil
}

func runMeetingFollowup(ctx context.Context, cfg Config, discord discordClient, ai openAIClient, candidates []Candidate, keys []string, meetingDate string, result Result) (Result, error) {
	for _, c := range candidates {
		if c.DoneReaction {
			result.Skipped = append(result.Skipped, c.ID+": Discord Done reaction")
			continue
		}
		first, err := ai.decide(ctx, c, keys)
		if err != nil {
			return result, fmt.Errorf("meeting followup decision for %s: %w", c.ID, err)
		}
		verified, err := ai.verify(ctx, c, first, keys)
		if err != nil {
			return result, fmt.Errorf("meeting followup verification for %s: %w", c.ID, err)
		}
		if verified.Certainty != "confirmed" || verified.Status != "open" {
			result.Skipped = append(result.Skipped, c.ID+": "+verified.Certainty+"/"+verified.Status)
			continue
		}
		if len(verified.EvidenceRefs) == 0 {
			result.Skipped = append(result.Skipped, c.ID+": no cited evidence")
			continue
		}
		known := map[string]bool{}
		for _, e := range c.Evidence {
			known[e.Ref] = true
		}
		valid := true
		for _, ref := range verified.EvidenceRefs {
			if !known[ref] {
				valid = false
			}
		}
		if !valid {
			result.Skipped = append(result.Skipped, c.ID+": unknown cited evidence")
			continue
		}
		item := MeetingFollowup{CandidateID: c.ID, Title: c.Title, NotionURL: c.URL}
		if verified.DueDate != nil {
			due := *verified.DueDate
			parsed, err := time.Parse("2006-01-02", due)
			if err != nil || parsed.Format("2006-01-02") != due {
				result.Skipped = append(result.Skipped, c.ID+": invalid due date")
				continue
			}
			item.DueDate = due
		}
		for _, key := range verified.AssigneeKeys {
			if !containsString(c.AssigneeKeys, key) {
				continue
			}
			id := cfg.UserMap[key]
			if !snowflake(id) {
				valid = false
				break
			}
			item.UserIDs = append(item.UserIDs, id)
		}
		if !valid {
			result.Skipped = append(result.Skipped, c.ID+": assignee mapping missing")
			continue
		}
		item.UserIDs = uniqueStrings(item.UserIDs)
		if len(c.ThreadIDs) > 0 {
			item.ThreadURL = "https://discord.com/channels/" + discord.guildID + "/" + c.ThreadIDs[0]
		}
		result.Followups = append(result.Followups, item)
	}
	result.Eligible = len(result.Followups)
	prepared := make([]string, len(result.Followups))
	for i, item := range result.Followups {
		content, err := discord.formatFollowup(meetingDate, item)
		if err != nil {
			return result, err
		}
		prepared[i] = content
	}
	recent, err := discord.recentReminders(ctx)
	if err != nil {
		return result, fmt.Errorf("Discord followup history: %w", err)
	}
	pending := []MeetingFollowup{}
	pendingContent := []string{}
	for i, item := range result.Followups {
		if discord.alreadySentFollowup(recent, meetingDate, item) {
			result.AlreadySent++
			continue
		}
		pending = append(pending, item)
		pendingContent = append(pendingContent, prepared[i])
	}
	result.Followups = pending
	if cfg.DryRun {
		return result, nil
	}
	for i, item := range pending {
		latest, err := discord.recentReminders(ctx)
		if err != nil {
			return result, fmt.Errorf("Discord followup pre-send check: %w", err)
		}
		if discord.alreadySentFollowup(latest, meetingDate, item) {
			result.AlreadySent++
			continue
		}
		if _, err := discord.sendDigest(ctx, pendingContent[i], item.UserIDs); err != nil {
			checkCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			after, checkErr := discord.recentReminders(checkCtx)
			cancel()
			if checkErr == nil && discord.alreadySentFollowup(after, meetingDate, item) {
				result.Sent++
				continue
			}
			return result, fmt.Errorf("Discord followup send uncertain for %s; inspect channel before retry: %w", item.CandidateID, err)
		}
		result.Sent++
	}
	return result, nil
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
