package config

import (
	"fmt"
	"os"
	"strings"
)

const DefaultDiscordTokenFile = "/var/run/secrets/discord-bot/token"

type Config struct {
	DiscordToken     string
	DiscordTokenFile string
}

func Load() (Config, error) {
	tokenFile := os.Getenv("DISCORD_TOKEN_FILE")
	if tokenFile == "" {
		tokenFile = DefaultDiscordTokenFile
	}

	token, err := os.ReadFile(tokenFile)
	if err != nil {
		return Config{}, fmt.Errorf("read Discord token file %q: %w", tokenFile, err)
	}

	discordToken := strings.TrimSpace(string(token))
	if discordToken == "" {
		return Config{}, fmt.Errorf("Discord token file %q is empty", tokenFile)
	}

	return Config{
		DiscordToken:     discordToken,
		DiscordTokenFile: tokenFile,
	}, nil
}
