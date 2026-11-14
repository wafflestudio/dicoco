package opsreminder

import (
	"context"
	"fmt"
	"sort"
	"time"
)

func Run(ctx context.Context, cfg Config) (Result, error) {
	return runWithAPI(ctx, cfg, newAPIClient())
}

func runWithAPI(ctx context.Context, cfg Config, api *apiClient) (Result, error) {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return Result{}, err
	}
	cfg.Now = cfg.Now.In(loc)
	if cfg.Mode == "daily" || cfg.Mode == "notion-sync" {
		return runDailyPipeline(ctx, cfg, api)
	}
	result := Result{DryRun: cfg.DryRun}
	notion := notionCollector{api: api, token: cfg.NotionToken, meetingID: cfg.MeetingDataSourceID, now: cfg.Now}
	if cfg.Mode == "meeting-followup" {
		notion.meetingDate = previousSaturday(cfg.Now).Format("2006-01-02")
	}
	discord := discordClient{api: api, token: cfg.DiscordToken, sourceID: cfg.SourceChannelID, reminderID: cfg.ReminderChannelID, now: cfg.Now}
	if err := discord.initialize(ctx); err != nil {
		return result, fmt.Errorf("Discord setup: %w", err)
	}
	candidates, err := notion.collect(ctx)
	if err != nil {
		return result, err
	}
	result.Collected = len(candidates)
	if len(candidates) > 60 {
		return result, fmt.Errorf("%d candidates exceed safe daily limit of 60", len(candidates))
	}
	if cfg.TestLimit > 0 {
		result.TestMode = true
		sort.SliceStable(candidates, func(i, j int) bool {
			return candidates[i].Evidence[0].At.After(candidates[j].Evidence[0].At)
		})
		if len(candidates) > cfg.TestLimit {
			candidates = candidates[:cfg.TestLimit]
		}
	}
	result.Processed = len(candidates)
	if err := discord.attachSourceThreads(ctx, candidates); err != nil {
		return result, err
	}
	if err := discord.attachThreadEvidence(ctx, candidates); err != nil {
		return result, err
	}
	keys := make([]string, 0, len(cfg.UserMap))
	for key := range cfg.UserMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ai := openAIClient{api: api, key: cfg.OpenRouterKey, model: cfg.OpenRouterModel, now: cfg.Now, endpoint: "https://openrouter.ai/api/v1/responses"}
	if cfg.OpenAIKey != "" {
		ai.key = cfg.OpenAIKey
		ai.model = cfg.OpenAIModel
		ai.endpoint = "https://api.openai.com/v1/responses"
	}
	if cfg.Mode == "meeting-followup" {
		ai.mode = "meeting-followup"
		return runMeetingFollowup(ctx, cfg, discord, ai, candidates, keys, notion.meetingDate, result)
	}
	tomorrow := cfg.Now.AddDate(0, 0, 1).Format("2006-01-02")
	for _, c := range candidates {
		if c.DoneReaction {
			result.Skipped = append(result.Skipped, c.ID+": Discord Done reaction")
			continue
		}
		d, err := ai.decide(ctx, c, keys)
		if err != nil {
			return result, fmt.Errorf("model decision for %s: %w", c.ID, err)
		}
		v, err := ai.verify(ctx, c, d, keys)
		if err != nil {
			return result, fmt.Errorf("model verification for %s: %w", c.ID, err)
		}
		if v.Certainty == "uncertain" {
			if v.DueDate != nil && *v.DueDate == tomorrow && v.Status != "done" && v.Status != "canceled" {
				result.Uncertain = append(result.Uncertain, UncertainItem{CandidateID: c.ID, Title: c.Title, DueDate: tomorrow, NotionURL: c.URL, Reason: v.Reason})
			} else {
				result.Skipped = append(result.Skipped, c.ID+": uncertain; not clearly due tomorrow")
			}
			continue
		}
		r, reason := validDecision(c, v.Decision(), cfg.UserMap, tomorrow)
		if reason != "" {
			result.Skipped = append(result.Skipped, c.ID+": "+reason)
			continue
		}
		if len(c.ThreadIDs) > 0 {
			r.ThreadURL = "https://discord.com/channels/" + discord.guildID + "/" + c.ThreadIDs[0]
		}
		if _, err := discord.format(r); err != nil {
			result.Skipped = append(result.Skipped, c.ID+": "+err.Error())
			continue
		}
		result.Reminders = append(result.Reminders, r)
	}
	result.Eligible = len(result.Reminders)
	// Read the channel only after all source and model checks have succeeded.
	recent, err := discord.recentReminders(ctx)
	if err != nil {
		return result, fmt.Errorf("Discord reminder history: %w", err)
	}
	pending := []Reminder{}
	for _, r := range result.Reminders {
		if discord.alreadySent(recent, r) {
			result.AlreadySent++
			continue
		}
		pending = append(pending, r)
	}
	result.Reminders = pending
	if len(pending) == 0 && len(result.Uncertain) == 0 && !result.TestMode {
		return result, nil
	}
	content, mentionIDs, err := discord.formatDigest(tomorrow, pending, result.Uncertain, result.TestMode, result.Processed, result.Collected)
	if err != nil {
		return result, err
	}
	if cfg.DryRun {
		return result, nil
	}
	latest, err := discord.recentReminders(ctx)
	if err != nil {
		return result, fmt.Errorf("Discord pre-send check: %w", err)
	}
	if discord.alreadySentDigest(latest, tomorrow, result.TestMode) {
		result.AlreadySent++
		return result, nil
	}
	if _, err := discord.sendDigest(ctx, content, mentionIDs); err != nil {
		checkCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		after, checkErr := discord.recentReminders(checkCtx)
		cancel()
		if checkErr == nil && discord.alreadySentDigest(after, tomorrow, result.TestMode) {
			result.Sent++
			return result, nil
		}
		return result, fmt.Errorf("Discord send uncertain; inspect channel before retry: %w", err)
	}
	result.Sent++
	return result, nil
}
