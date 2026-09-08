package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDiscordToken(t *testing.T) {
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
