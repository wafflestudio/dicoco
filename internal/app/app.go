package app

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wafflestudio/discord/internal/config"
	"github.com/wafflestudio/discord/internal/discord"
	"github.com/wafflestudio/discord/internal/feature/ping"
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
	discordClient.Register(ping.New())

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
