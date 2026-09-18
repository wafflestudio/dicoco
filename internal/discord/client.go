package discord

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

type Module interface { // Register메소드 있으면 모두 Module 취급. 따로 선언 안함.
	Register(*discordgo.Session)
}

type Client struct {
	session *discordgo.Session
}

func NewClient(token string) (*Client, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create Discord session: %w", err)
	}

	session.Identify.Intents =
		discordgo.IntentsGuilds |
			discordgo.IntentsGuildMessages |
			discordgo.IntentsDirectMessages |
			discordgo.IntentsMessageContent |
			discordgo.IntentsGuildMessageReactions

	return &Client{session: session}, nil
}

func (c *Client) Register(module Module) {
	module.Register(c.session)
	log.Printf("[bot] registered %T", module)
}

func (c *Client) Open() error {
	if err := c.session.Open(); err != nil {
		return fmt.Errorf("open Discord Gateway connection: %w", err)
	}
	return nil
}

func (c *Client) Close() error {
	if err := c.session.Close(); err != nil {
		return fmt.Errorf("close Discord Gateway connection: %w", err)
	}
	return nil
}

func (c *Client) User() (string, string) {
	if c.session.State == nil || c.session.State.User == nil {
		return "", ""
	}
	return c.session.State.User.ID, c.session.State.User.Username
}
