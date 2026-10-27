package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDiscordToken(t *testing.T) {
	t.Setenv("AGENT_ENABLED", "false")
	t.Setenv("AGENT_ONLY", "")
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DISCORD_TOKEN_FILE", tokenFile)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.DiscordToken != "test-token" {
		t.Fatalf("unexpected Discord token: %q", cfg.DiscordToken)
	}
	if cfg.DiscordTokenFile != tokenFile {
		t.Fatalf("unexpected Discord token file: %q", cfg.DiscordTokenFile)
	}
}

func TestAgentConfiguration(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISCORD_TOKEN_FILE", tokenFile)
	t.Setenv("AGENT_ENABLED", "")
	adminFile := filepath.Join(dir, "admin_2026_channel_id")
	if err := os.WriteFile(adminFile, []byte("123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_ONLY", "true")
	t.Setenv("OPENCODE_URL", "")
	t.Setenv("OPENCODE_BINARY", "")
	t.Setenv("OPENCODE_PASSWORD_FILE", "")
	t.Setenv("OPENCODE_DIRECTORY", dir)
	t.Setenv("GITHUB_APP_ID_FILE", filepath.Join(dir, "missing-id"))
	t.Setenv("GITHUB_APP_PRIVATE_KEY_FILE", filepath.Join(dir, "missing-key"))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenCodeBinary != "opencode" || cfg.OpenCodeURL != "" || cfg.OpenCodeDirectory != dir || !cfg.AgentOnly || !cfg.AgentEnabled {
		t.Fatalf("unexpected config %+v", cfg)
	}
	if err := os.Remove(adminFile); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil || !cfg.AgentEnabled || cfg.OpenCodeBinary != "opencode" {
		t.Fatal("agent must stay enabled without an admin channel")
	}
	t.Setenv("AGENT_ENABLED", "false")
	if _, err := Load(); err == nil {
		t.Fatal("agent-only and explicitly disabled should fail")
	}
	t.Setenv("AGENT_ONLY", "false")
	cfg, err = Load()
	if err != nil || cfg.AgentEnabled {
		t.Fatal("explicit opt-out should disable agent")
	}
}

func TestGitHubAppSecretFiles(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "discord-token")
	id := filepath.Join(dir, "app-id")
	key := filepath.Join(dir, "key.pem")
	for path, value := range map[string]string{token: "test-discord-token", id: "5490993\n", key: "test-key"} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DISCORD_TOKEN_FILE", token)
	t.Setenv("AGENT_ENABLED", "true")
	t.Setenv("AGENT_ONLY", "")
	t.Setenv("OPENCODE_URL", "")
	t.Setenv("OPENCODE_DIRECTORY", dir)
	t.Setenv("OPENCODE_PASSWORD_FILE", "")
	t.Setenv("GITHUB_APP_ID_FILE", id)
	t.Setenv("GITHUB_APP_PRIVATE_KEY_FILE", key)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHubAppID != "5490993" || cfg.GitHubPrivateKeyFile != key {
		t.Fatal("GitHub App files were not loaded")
	}
	t.Setenv("GITHUB_APP_ID_FILE", filepath.Join(dir, "missing"))
	if _, err := Load(); err == nil {
		t.Fatal("mounted key without App ID should produce a configuration error")
	}
}
