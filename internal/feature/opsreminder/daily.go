package opsreminder

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

func modelForRun(cfg Config, api *apiClient) openAIClient {
	ai := openAIClient{api: api, key: cfg.OpenRouterKey, model: cfg.OpenRouterModel, now: cfg.Now, endpoint: "https://openrouter.ai/api/v1/responses"}
	if cfg.OpenAIKey != "" {
		ai.key = cfg.OpenAIKey
		ai.model = cfg.OpenAIModel
		ai.endpoint = "https://api.openai.com/v1/responses"
	}
	return ai
}
func knownEvidence(c Candidate, refs []string) bool {
	if len(refs) == 0 {
		return false
	}
	known := map[string]bool{}
	for _, e := range c.Evidence {
		known[e.Ref] = true
	}
	for _, ref := range refs {
		if !known[ref] {
			return false
		}
	}
	return true
}

func verifiedClosedFromThread(c Candidate, v Verification) bool {
	if v.Certainty != "confirmed" || (v.Status != "done" && v.Status != "canceled") || !knownEvidence(c, v.EvidenceRefs) {
		return false
	}
	for _, ref := range v.EvidenceRefs {
		for _, e := range c.Evidence {
			if e.Ref == ref && e.Kind == "discord_thread_message" {
				return true
			}
		}
	}
	return false
}
func userKeys(cfg Config) []string {
	keys := make([]string, 0, len(cfg.UserMap))
	for k := range cfg.UserMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Stage 1: find the last held meeting and the nearest scheduled meeting by DB Date.
func findMeetingPair(ctx context.Context, n notionCollector, result *Result) (*scheduledMeeting, *scheduledMeeting, error) {
	source, target, err := n.meetingPair(ctx)
	if err != nil {
		return nil, nil, err
	}
	if source != nil {
		result.SourceMeetingDate = source.Date
	}
	if target != nil {
		result.NextMeetingDate = target.Date
	}
	return source, target, nil
}

// Stage 2: extract source decisions, then add only missing source links to the next meeting page.
func syncNextMeeting(ctx context.Context, cfg Config, n notionCollector, d discordClient, ai openAIClient, source, target *scheduledMeeting, result *Result) (string, error) {
	heading, err := n.todoHeading(ctx, target.Page, false)
	if err != nil {
		return "", err
	}
	existing, owners, err := n.managedTasks(ctx, heading, target.Page.URL, target.Date)
	if err != nil {
		return "", err
	}
	bySource := map[string]bool{}
	for _, t := range existing {
		if bySource[t.SourceURL] {
			return "", fmt.Errorf("duplicate managed TODO source %s", t.SourceURL)
		}
		bySource[t.SourceURL] = true
	}
	if source == nil || !sourceEditedOnMeetingDay(*source) {
		return heading, nil
	}
	planned := []managedTask{}
	sourceHeading, err := n.todoHeading(ctx, source.Page, false)
	if err != nil {
		return "", err
	}
	if sourceHeading != "" {
		previousTasks, _, err := n.managedTasks(ctx, sourceHeading, source.Page.URL, source.Date)
		if err != nil {
			return "", err
		}
		carryCfg := cfg
		if cfg.Mode == "notion-sync" {
			carryCfg.DryRun = true
		}
		carry, err := refreshCompletion(ctx, carryCfg, n, d, ai, previousTasks, result)
		if err != nil {
			return "", err
		}
		for _, task := range carry {
			if bySource[task.SourceURL] {
				continue
			}
			if task.DefaultDeadline {
				task.DueDate = target.Date
			}
			planned = append(planned, task)
			bySource[task.SourceURL] = true
			result.PlannedTasks = append(result.PlannedTasks, PipelineTask{Owner: task.Owner, Title: task.Title, DueDate: task.DueDate, DefaultDeadline: task.DefaultDeadline, SourceURL: task.SourceURL})
		}
	}
	collector := n
	collector.meetingDate = source.Date
	candidates, err := collector.collect(ctx)
	if err != nil {
		return "", err
	}
	result.Collected = len(candidates)
	if len(candidates) > 60 {
		return "", fmt.Errorf("meeting candidate count exceeds 60")
	}
	if cfg.TestLimit > 0 && len(candidates) > cfg.TestLimit {
		candidates = candidates[:cfg.TestLimit]
	}
	result.Processed = len(candidates)
	// A page can be prepared before the meeting. Require an agenda TODO touched on or after its Date.
	hasMeetingNote := false
	for _, c := range candidates {
		if len(c.Evidence) > 0 && c.Evidence[0].At.In(cfg.Now.Location()).Format("2006-01-02") >= source.Date {
			hasMeetingNote = true
			break
		}
	}
	if !hasMeetingNote {
		return heading, nil
	}
	if err := d.attachSourceThreads(ctx, candidates); err != nil {
		return "", err
	}
	if err := d.attachThreadEvidence(ctx, candidates); err != nil {
		return "", err
	}
	keys := userKeys(cfg)
	ai.mode = "meeting-extract"
	ai.nextMeetingDate = target.Date
	for _, c := range candidates {
		if bySource[c.URL] || c.DoneReaction {
			continue
		}
		first, err := ai.decide(ctx, c, keys)
		if err != nil {
			return "", err
		}
		v, err := ai.verify(ctx, c, first, keys)
		if err != nil {
			return "", err
		}
		if v.Certainty != "confirmed" || v.Status != "open" || !knownEvidence(c, v.EvidenceRefs) {
			continue
		}
		names := []string{}
		for _, key := range v.AssigneeKeys {
			if containsString(c.AssigneeKeys, key) && cfg.UserMap[key] != "" {
				names = append(names, key)
			}
		}
		names = uniqueStrings(names)
		sort.Strings(names)
		owner := "담당 미정"
		if len(names) > 0 {
			owner = strings.Join(names, ", ")
		}
		due := ""
		if v.DueDate != nil {
			date, err := time.Parse("2006-01-02", *v.DueDate)
			if err == nil && date.Format("2006-01-02") == *v.DueDate {
				due = *v.DueDate
			}
		}
		title := strings.TrimSpace(v.TaskTitle)
		if title == "" || len([]rune(title)) > 120 || strings.ContainsAny(title, "\r\n") || strings.Contains(title, "@") || strings.Contains(title, "http") {
			continue
		}
		defaultDeadline := due == ""
		if defaultDeadline {
			due = target.Date
		}
		t := managedTask{Owner: owner, Title: title, DueDate: due, DefaultDeadline: defaultDeadline, SourceURL: c.URL, ThreadIDs: c.ThreadIDs}
		if len(c.ThreadIDs) > 0 {
			t.ThreadURL = "https://discord.com/channels/" + d.guildID + "/" + c.ThreadIDs[0]
		}
		planned = append(planned, t)
		bySource[c.URL] = true
		result.PlannedTasks = append(result.PlannedTasks, PipelineTask{Owner: owner, Title: t.Title, DueDate: due, DefaultDeadline: defaultDeadline, SourceURL: t.SourceURL})
	}
	if cfg.DryRun || len(planned) == 0 {
		return heading, nil
	}
	if heading == "" {
		heading, err = n.todoHeading(ctx, target.Page, true)
		if err != nil {
			return "", err
		}
	}
	for _, t := range planned {
		// A failed append can have succeeded remotely. The next run reads source links before writing.
		if group := owners[t.Owner]; group != "" {
			if _, err = n.appendChildren(ctx, group, []any{todoBlock(t)}, ""); err != nil {
				return "", err
			}
		} else {
			added, err := n.appendChildren(ctx, heading, []any{ownerBlock(t.Owner, todoBlock(t))}, "")
			if err != nil {
				return "", err
			}
			if len(added) != 1 || added[0].ID == "" {
				return "", fmt.Errorf("owner group creation unconfirmed")
			}
			owners[t.Owner] = added[0].ID
		}
		result.CreatedTasks++
	}
	return heading, nil
}

func taskCandidate(ctx context.Context, n notionCollector, t managedTask) (Candidate, error) {
	b, err := n.sourceBlock(ctx, t.SourceURL)
	if err != nil {
		return Candidate{}, err
	}
	sourceText, _, _ := blockContent(b)
	c := Candidate{ID: t.BlockID, URL: t.SourceURL, Title: t.Title, AssigneeKeys: []string{t.Owner}, ThreadIDs: uniqueStrings(append(t.ThreadIDs, threadIDs(sourceText)...)), Evidence: []Evidence{
		notionEvidence(t.BlockID, t.URL, t.Title, "managed_todo", t.LastEdited),
		notionEvidence(b.ID, t.SourceURL, sourceText, "source_meeting", b.LastEdited),
	}}
	return c, nil
}

// Stage 3: update the binary Notion checkbox only for a verified completion or cancellation.
func refreshCompletion(ctx context.Context, cfg Config, n notionCollector, d discordClient, ai openAIClient, tasks []managedTask, result *Result) ([]managedTask, error) {
	open := []managedTask{}
	candidates := []Candidate{}
	indexes := []int{}
	for i, t := range tasks {
		if t.Checked {
			continue
		}
		c, err := taskCandidate(ctx, n, t)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
		indexes = append(indexes, i)
	}
	if err := d.attachSourceThreads(ctx, candidates); err != nil {
		return nil, err
	}
	if err := d.attachThreadEvidence(ctx, candidates); err != nil {
		return nil, err
	}
	ai.mode = "daily-status"
	keys := userKeys(cfg)
	for j, c := range candidates {
		t := tasks[indexes[j]]
		done := c.DoneReaction
		if !done {
			first, err := ai.decide(ctx, c, keys)
			if err != nil {
				return nil, err
			}
			v, err := ai.verify(ctx, c, first, keys)
			if err != nil {
				return nil, err
			}
			done = verifiedClosedFromThread(c, v)
		}
		if done {
			if !cfg.DryRun {
				if err := n.setChecked(ctx, t.BlockID); err != nil {
					return nil, err
				}
			}
			result.CheckedTasks++
			continue
		}
		open = append(open, t)
	}
	return open, nil
}

func (d discordClient) alreadySentDaily(messages []discordMessage, content string) bool {
	for _, m := range messages {
		if m.Author.ID == d.botID && m.Content == content {
			return true
		}
	}
	return false
}
func formatDailyReminder(date, meetingDate string, t managedTask, users []string) (string, error) {
	reason := []string{}
	if meetingDate == date {
		reason = append(reason, "회의 하루 전")
	}
	if t.DueDate == date {
		reason = append(reason, "마감 하루 전")
	}
	if len(reason) == 0 {
		return "", fmt.Errorf("task is not due for reminder")
	}
	mentions := []string{}
	for _, id := range users {
		if !snowflake(id) {
			return "", fmt.Errorf("invalid user ID")
		}
		mentions = append(mentions, "<@"+id+">")
	}
	due := t.DueDate
	if t.DefaultDeadline {
		due = "다음 회의 전 (" + meetingDate + ")"
	}
	label := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]").Replace(safeDiscordText(t.Title))
	content := strings.TrimSpace(strings.Join(mentions, " ")+" ["+label+"]("+t.URL+")") + "\n기한: " + due
	if len(utf16.Encode([]rune(content))) > 1900 {
		return "", fmt.Errorf("daily reminder exceeds Discord limit")
	}
	return content, nil
}
func ownerIDs(owner string, mapping map[string]string) ([]string, error) {
	if owner == "담당 미정" {
		return nil, nil
	}
	ids := []string{}
	for _, name := range strings.Split(owner, ",") {
		name = strings.TrimSpace(name)
		id := mapping[name]
		if !snowflake(id) {
			return nil, fmt.Errorf("unknown TODO owner %s", name)
		}
		ids = append(ids, id)
	}
	return uniqueStrings(ids), nil
}

// Stage 4: send open tasks only on the calendar day before the meeting or their deadline.
func sendDueReminders(ctx context.Context, cfg Config, d discordClient, meetingDate string, tasks []managedTask, result *Result) error {
	tomorrow := cfg.Now.AddDate(0, 0, 1).Format("2006-01-02")
	type ready struct {
		task    managedTask
		content string
		users   []string
	}
	pending := []ready{}
	for _, t := range tasks {
		if meetingDate != tomorrow && t.DueDate != tomorrow {
			continue
		}
		ids, err := ownerIDs(t.Owner, cfg.UserMap)
		if err != nil {
			return err
		}
		content, err := formatDailyReminder(tomorrow, meetingDate, t, ids)
		if err != nil {
			return err
		}
		pending = append(pending, ready{t, content, ids})
	}
	result.Eligible = len(pending)
	if len(pending) == 0 {
		return nil
	}
	recent, err := d.recentReminders(ctx)
	if err != nil {
		return err
	}
	for _, item := range pending {
		if d.alreadySentDaily(recent, item.content) {
			result.AlreadySent++
			continue
		}
		if cfg.DryRun {
			result.Reminders = append(result.Reminders, Reminder{CandidateID: item.task.BlockID, Title: item.task.Title, DueDate: item.task.DueDate, NotionURL: item.task.URL, UserIDs: item.users})
			continue
		}
		latest, err := d.recentReminders(ctx)
		if err != nil {
			return err
		}
		if d.alreadySentDaily(latest, item.content) {
			result.AlreadySent++
			continue
		}
		if _, err = d.sendDigest(ctx, item.content, item.users); err != nil {
			checkCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			after, e := d.recentReminders(checkCtx)
			cancel()
			if e == nil && d.alreadySentDaily(after, item.content) {
				result.Sent++
				continue
			}
			return fmt.Errorf("Discord reminder send uncertain for %s: %w", item.task.BlockID, err)
		}
		result.Sent++
	}
	return nil
}

func runDailyPipeline(ctx context.Context, cfg Config, api *apiClient) (Result, error) {
	result := Result{DryRun: cfg.DryRun, TestMode: cfg.TestLimit > 0}
	n := notionCollector{api: api, token: cfg.NotionToken, meetingID: cfg.MeetingDataSourceID, now: cfg.Now}
	source, target, err := findMeetingPair(ctx, n, &result)
	if err != nil {
		return result, err
	}
	if target == nil {
		result.Skipped = append(result.Skipped, "no upcoming meeting page with a Date")
		return result, nil
	}
	d := discordClient{api: api, token: cfg.DiscordToken, sourceID: cfg.SourceChannelID, reminderID: cfg.ReminderChannelID, now: cfg.Now}
	if err := d.initialize(ctx); err != nil {
		return result, err
	}
	ai := modelForRun(cfg, api)
	heading, err := syncNextMeeting(ctx, cfg, n, d, ai, source, target, &result)
	if err != nil {
		return result, err
	}
	if cfg.Mode == "notion-sync" {
		return result, nil
	}
	if heading == "" {
		return result, nil
	}
	tasks, _, err := n.managedTasks(ctx, heading, target.Page.URL, target.Date)
	if err != nil {
		return result, err
	}
	if len(tasks) > 60 {
		return result, fmt.Errorf("managed TODO count exceeds 60")
	}
	open, err := refreshCompletion(ctx, cfg, n, d, ai, tasks, &result)
	if err != nil {
		return result, err
	}
	if err := sendDueReminders(ctx, cfg, d, target.Date, open, &result); err != nil {
		return result, err
	}
	return result, nil
}
