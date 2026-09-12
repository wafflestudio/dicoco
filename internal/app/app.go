package app

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wafflestudio/dicoco/internal/config"
	"github.com/wafflestudio/dicoco/internal/discord"
	"github.com/wafflestudio/dicoco/internal/feature/reference"
	"github.com/wafflestudio/dicoco/internal/feature/reference/onreaction"
	"github.com/wafflestudio/dicoco/internal/feature/reference/ping"
)

func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	discordClient, err := discord.NewClient(cfg.DiscordToken)
	if err != nil {
		return err
	}
	discordClient.Register(reference.New())
	discordClient.Register(ping.New())
	discordClient.Register(onreaction.New())

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
	<-ctx.Done()
	log.Println("stopping bot")

	return discordClient.Close()
}
