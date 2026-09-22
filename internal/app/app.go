package app

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wafflestudio/dicoco/internal/config"
	"github.com/wafflestudio/dicoco/internal/discord"
	"github.com/wafflestudio/dicoco/internal/feature/admin/notion"
	"github.com/wafflestudio/dicoco/internal/feature/reference/dm"
	"github.com/wafflestudio/dicoco/internal/feature/reference/onreaction"
	"github.com/wafflestudio/dicoco/internal/feature/reference/ping"
	"github.com/wafflestudio/dicoco/internal/feature/scratch"
	"github.com/wafflestudio/dicoco/internal/feature/waffle"
)

const waffleEnabled = false

func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	discordClient, err := discord.NewClient(cfg.DiscordToken)
	if err != nil {
		return err
	}

	// Feature registration starts here.
	notionHandler, err := notion.New()
	if err != nil {
		return err
	}
	discordClient.Register(notionHandler)

	discordClient.Register(dm.New())
	discordClient.Register(ping.New())
	discordClient.Register(onreaction.New())
	scratchHandler := scratch.New()
	discordClient.Register(scratchHandler)
	var waffleHandler *waffle.Handler
	if waffleEnabled {
		waffleHandler, err = waffle.New()
		if err != nil {
			return err
		}
		discordClient.Register(waffleHandler)
	}
	// Feature registration ends here.

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := discordClient.Open(); err != nil {
		return err
	}

	userID, username := discordClient.User()
	log.Printf("bot connected as %s (%s)", username, userID)
	go scratchHandler.Run(ctx)
	if waffleHandler != nil {
		go waffleHandler.Run(ctx)
	}
	<-ctx.Done()
	log.Println("stopping bot")

	return discordClient.Close()
}
