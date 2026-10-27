package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wafflestudio/dicoco/internal/config"
	"github.com/wafflestudio/dicoco/internal/discord"
	"github.com/wafflestudio/dicoco/internal/feature/admin/notion"
	"github.com/wafflestudio/dicoco/internal/feature/agent"
	"github.com/wafflestudio/dicoco/internal/feature/emoji/tada"
	"github.com/wafflestudio/dicoco/internal/feature/ladder"
	"github.com/wafflestudio/dicoco/internal/feature/reference/dm"
	githubpr "github.com/wafflestudio/dicoco/internal/feature/reference/github_pr"
	"github.com/wafflestudio/dicoco/internal/feature/reference/onreaction"
	"github.com/wafflestudio/dicoco/internal/feature/scratch"
	"github.com/wafflestudio/dicoco/internal/feature/waffle"
	"github.com/wafflestudio/dicoco/internal/githubauth"
	"github.com/wafflestudio/dicoco/internal/monitor"
	"github.com/wafflestudio/dicoco/internal/opencode"
)

const waffleEnabled = false

func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go monitor.Run(ctx)

	discordClient, err := discord.NewClient(cfg.DiscordToken)
	if err != nil {
		return err
	}

	// Feature registration starts here.
	discordClient.Register(dm.New())
	var agentHandler *agent.Handler
	if cfg.AgentEnabled {
		var engineEnvironment []string
		if cfg.GitHubAppID != "" {
			if cfg.OpenCodeURL != "" {
				return fmt.Errorf("managed GitHub App auth requires local OpenCode; leave OPENCODE_URL unset")
			}
			gitHubApp, err := githubauth.New(cfg.GitHubAppID, cfg.GitHubPrivateKeyFile)
			if err != nil {
				return err
			}
			dir, closeAuth, err := gitHubApp.Start(ctx)
			if err != nil {
				return err
			}
			defer closeAuth()
			engineEnvironment = []string{"GH_CONFIG_DIR=" + dir, "GH_PROMPT_DISABLED=1", "GH_PAGER=cat", "GH_NO_UPDATE_NOTIFIER=1"}
		}
		var engine *opencode.Client
		if cfg.OpenCodeURL == "" {
			var closeEngine func()
			engine, closeEngine, err = opencode.Start(ctx, cfg.OpenCodeBinary, cfg.OpenCodeDirectory, engineEnvironment...)
			if err != nil {
				return err
			}
			defer closeEngine()
		} else {
			engine, err = opencode.New(cfg.OpenCodeURL, cfg.OpenCodePassword, cfg.OpenCodeDirectory)
			if err != nil {
				return err
			}
		}
		healthCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = engine.Health(healthCtx)
		cancel()
		if err != nil {
			return err
		}
		agentHandler = agent.New(engine)
		discordClient.Register(agentHandler)
	}
	var scratchHandler *scratch.Handler
	var waffleHandler *waffle.Handler
	if !cfg.AgentOnly {
		notionHandler, err := notion.New()
		if err != nil {
			return err
		}
		discordClient.Register(notionHandler)

		discordClient.Register(onreaction.New())
		discordClient.Register(githubpr.New())
		discordClient.Register(tada.New())
		scratchHandler = scratch.New()
		discordClient.Register(scratchHandler)
		discordClient.Register(ladder.New(scratchHandler.Mine))
		if waffleEnabled {
			waffleHandler, err = waffle.New()
			if err != nil {
				return err
			}
			discordClient.Register(waffleHandler)
		}
	}
	// Feature registration ends here.

	if err := discordClient.Open(); err != nil {
		return err
	}

	userID, username := discordClient.User()
	log.Printf("bot connected as %s (%s)", username, userID)
	if agentHandler != nil {
		agentCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { defer close(done); agentHandler.Run(agentCtx) }()
		defer func() { cancel(); <-done }()
	}
	if scratchHandler != nil {
		go scratchHandler.Run(ctx)
	}
	if waffleHandler != nil {
		go waffleHandler.Run(ctx)
	}
	<-ctx.Done()
	log.Println("stopping bot")

	return discordClient.Close()
}
