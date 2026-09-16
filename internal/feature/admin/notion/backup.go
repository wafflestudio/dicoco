package notion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	announceChannelIDFile = "/var/run/secrets/discord-bot/announce_channel_id"
	n8nWebhookURLFile     = "/var/run/secrets/discord-bot/n8n_webhook_url"
	n8nTokenFile          = "/var/run/secrets/discord-bot/n8n_token"
)

type Handler struct {
	announceChannelID string
	n8nWebhookURL     string
	n8nToken          string
	httpClient        *http.Client
}

func New() (*Handler, error) {
	announceChannelID, err := readMountedValue(announceChannelIDFile)
	if err != nil {
		return nil, fmt.Errorf("read announce channel ID: %w", err)
	}

	n8nWebhookURL, err := readMountedValue(n8nWebhookURLFile)
	if err != nil {
		return nil, fmt.Errorf("read n8n webhook URL: %w", err)
	}

	n8nToken, err := readMountedValue(n8nTokenFile)
	if err != nil {
		return nil, fmt.Errorf("read n8n token: %w", err)
	}

	return &Handler{
		announceChannelID: announceChannelID,
		n8nWebhookURL:     n8nWebhookURL,
		n8nToken:          n8nToken,
		httpClient:        &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.announceBackup)
}

func (h *Handler) announceBackup(session *discordgo.Session, message *discordgo.MessageCreate) {
	if message.Author == nil || message.Author.Bot {
		return
	}
	if message.ChannelID != h.announceChannelID {
		return
	}

	body, err := json.Marshal(message.Message)
	if err != nil {
		log.Printf("marshal Discord message %s: %v", message.ID, err)
		return
	}

	request, err := http.NewRequest(
		http.MethodPost,
		h.n8nWebhookURL,
		bytes.NewReader(body),
	)

	if err != nil {
		log.Printf("create HTTP request for Discord message %s: %v", message.ID, err)
		return
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("bot_to_n8n", h.n8nToken)

	response, err := h.httpClient.Do(request)

	if err != nil {
		log.Printf("send HTTP request for Discord message %s: %v", message.ID, err)
		return
	}
	defer response.Body.Close()

	_, _ = io.Copy(io.Discard, response.Body)

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		log.Printf("HTTP request for Discord message %s returned status %d", message.ID, response.StatusCode)
		return
	}
}

func readMountedValue(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	value := strings.TrimSpace(string(contents))
	if value == "" {
		return "", fmt.Errorf("file %q is empty", path)
	}

	return value, nil
}
