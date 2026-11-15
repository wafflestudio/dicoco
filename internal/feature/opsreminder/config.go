package opsreminder

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

const defaultDiscordTokenFile = "/var/run/secrets/discord-bot/token"

//go:embed members.json
var defaultUserMapJSON []byte

type Config struct {
	NotionToken         string
	DiscordToken        string
	OpenRouterKey       string
	OpenRouterModel     string
	OpenAIKey           string
	OpenAIModel         string
	MeetingDataSourceID string
	SourceChannelID     string
	ReminderChannelID   string
	UserMap             map[string]string
	Now                 time.Time
	DryRun              bool
	TestLimit           int
	Mode                string
}

func LoadConfig(now time.Time, dryRun bool) (Config, error) {
	c := Config{
		NotionToken:         strings.TrimSpace(os.Getenv("NOTION_TOKEN")),
		DiscordToken:        strings.TrimSpace(os.Getenv("DISCORD_TOKEN")),
		OpenRouterKey:       strings.TrimSpace(os.Getenv("OPENROUTER_KEY")),
		OpenRouterModel:     strings.TrimSpace(os.Getenv("OPENROUTER_MODEL")),
		OpenAIKey:           strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
		OpenAIModel:         strings.TrimSpace(os.Getenv("OPENAI_MODEL")),
		MeetingDataSourceID: strings.TrimSpace(os.Getenv("NOTION_MEETING_DATA_SOURCE_ID")),
		SourceChannelID:     strings.TrimSpace(os.Getenv("DISCORD_SOURCE_CHANNEL_ID")),
		ReminderChannelID:   strings.TrimSpace(os.Getenv("DISCORD_REMINDER_CHANNEL_ID")),
		Now:                 now, DryRun: dryRun,
	}
	if c.OpenRouterModel == "" {
		c.OpenRouterModel = "openai/gpt-4.1-mini"
	}
	if c.OpenAIModel == "" {
		c.OpenAIModel = "gpt-5-mini"
	}
	if c.DiscordToken == "" {
		path := os.Getenv("DISCORD_TOKEN_FILE")
		if path == "" {
			path = defaultDiscordTokenFile
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return c, errors.New("DISCORD_TOKEN or readable DISCORD_TOKEN_FILE is required")
		}
		c.DiscordToken = strings.TrimSpace(string(b))
	}
	for _, item := range []struct{ name, value string }{
		{"NOTION_TOKEN", c.NotionToken}, {"DISCORD_TOKEN", c.DiscordToken},
		{"NOTION_MEETING_DATA_SOURCE_ID", c.MeetingDataSourceID},
		{"DISCORD_SOURCE_CHANNEL_ID", c.SourceChannelID},
		{"DISCORD_REMINDER_CHANNEL_ID", c.ReminderChannelID},
	} {
		if item.value == "" {
			return c, errors.New(item.name + " is required")
		}
	}
	if c.OpenRouterKey == "" && c.OpenAIKey == "" {
		return c, errors.New("OPENROUTER_KEY or OPENAI_API_KEY is required")
	}
	mapJSON := defaultUserMapJSON
	if override := strings.TrimSpace(os.Getenv("DISCORD_USER_MAP_JSON")); override != "" {
		mapJSON = []byte(override)
	}
	if err := json.Unmarshal(mapJSON, &c.UserMap); err != nil || len(c.UserMap) == 0 {
		return c, errors.New("Discord user mapping must be a nonempty JSON object")
	}
	for key, id := range c.UserMap {
		if strings.TrimSpace(key) == "" || !snowflake(id) {
			return c, errors.New("DISCORD_USER_MAP_JSON has an invalid entry")
		}
	}
	return c, nil
}

func snowflake(s string) bool {
	if len(s) < 15 || len(s) > 22 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
