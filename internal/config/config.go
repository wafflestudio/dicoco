package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultDiscordTokenFile = "/var/run/secrets/discord-bot/token"

type Config struct {
	DiscordToken         string
	DiscordTokenFile     string
	AgentEnabled         bool
	AgentOnly            bool
	OpenCodeURL          string
	OpenCodePassword     string
	OpenCodeDirectory    string
	OpenCodeBinary       string
	OpenCodeModel        string
	GitHubAppID          string
	GitHubPrivateKeyFile string
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

	cfg := Config{
		DiscordToken:      discordToken,
		DiscordTokenFile:  tokenFile,
		AgentEnabled:      true,
		AgentOnly:         os.Getenv("AGENT_ONLY") == "true",
		OpenCodeURL:       os.Getenv("OPENCODE_URL"),
		OpenCodeDirectory: os.Getenv("OPENCODE_DIRECTORY"),
		OpenCodeBinary:    os.Getenv("OPENCODE_BINARY"),
		OpenCodeModel:     strings.TrimSpace(os.Getenv("OPENCODE_MODEL")),
	}
	if value := os.Getenv("AGENT_ENABLED"); value != "" {
		cfg.AgentEnabled, err = strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse AGENT_ENABLED: %w", err)
		}
	}
	if cfg.AgentOnly && !cfg.AgentEnabled {
		return Config{}, fmt.Errorf("AGENT_ONLY requires AGENT_ENABLED")
	}
	if cfg.AgentEnabled {
		idFile := os.Getenv("GITHUB_APP_ID_FILE")
		if idFile == "" {
			idFile = "/var/run/secrets/discord-bot/github_app_id"
		}
		cfg.GitHubPrivateKeyFile = os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE")
		if cfg.GitHubPrivateKeyFile == "" {
			cfg.GitHubPrivateKeyFile = "/var/run/secrets/github-app/private-key.pem"
		}
		id, idErr := os.ReadFile(idFile)
		_, keyErr := os.Stat(cfg.GitHubPrivateKeyFile)
		if !os.IsNotExist(idErr) || !os.IsNotExist(keyErr) {
			if idErr != nil {
				return Config{}, fmt.Errorf("read GitHub App ID file: %w", idErr)
			}
			if keyErr != nil {
				return Config{}, fmt.Errorf("read GitHub App key file: %w", keyErr)
			}
			cfg.GitHubAppID = strings.TrimSpace(string(id))
			if cfg.GitHubAppID == "" {
				return Config{}, fmt.Errorf("GitHub App ID file is empty")
			}
		}
		if cfg.OpenCodeBinary == "" {
			cfg.OpenCodeBinary = "opencode"
		}
		if cfg.OpenCodeDirectory == "" {
			cfg.OpenCodeDirectory = "."
		}
		if cfg.OpenCodeURL == "" {
			cfg.OpenCodeDirectory, err = filepath.Abs(cfg.OpenCodeDirectory)
			if err != nil {
				return Config{}, err
			}
			if info, err := os.Stat(cfg.OpenCodeDirectory); err != nil || !info.IsDir() {
				return Config{}, fmt.Errorf("OPENCODE_DIRECTORY must be an existing directory")
			}
		} else if !filepath.IsAbs(cfg.OpenCodeDirectory) {
			return Config{}, fmt.Errorf("external OpenCode requires an absolute OPENCODE_DIRECTORY on that server")
		}
		if path := os.Getenv("OPENCODE_PASSWORD_FILE"); path != "" {
			password, err := os.ReadFile(path)
			if err != nil {
				return Config{}, fmt.Errorf("read OpenCode password: %w", err)
			}
			cfg.OpenCodePassword = strings.TrimSpace(string(password))
			if cfg.OpenCodePassword == "" {
				return Config{}, fmt.Errorf("OpenCode password file is empty")
			}
		}
	}
	return cfg, nil
}
