package waffle

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	customEmojiName = "waffle"
	unicodeEmoji    = "🧇"

	jsonURLFile               = "/var/run/secrets/discord-bot/oci_waffle_json_url"
	announcementChannelIDFile = "/var/run/secrets/discord-bot/admin_2026_channel_id"
)

type Handler struct {
	logger                *log.Logger
	now                   func() time.Time
	announcementChannelID string
	loadMessage           func(*discordgo.Session, string, string) (*discordgo.Message, error)
	sendMessage           func(string, string) error
	store                 stateStore
	stateMu               sync.Mutex
}

func New() (*Handler, error) {
	jsonURL, err := readJSONURL(jsonURLFile)
	if err != nil {
		return nil, fmt.Errorf("[waffle] 오류: 저장소 설정 확인 실패")
	}
	announcementChannelID, err := readMountedValue(announcementChannelIDFile)
	if err != nil {
		return nil, fmt.Errorf("[waffle] 오류: 채널 설정 확인 실패")
	}

	return &Handler{
		logger:                log.Default(),
		now:                   time.Now,
		announcementChannelID: announcementChannelID,
		loadMessage: func(session *discordgo.Session, channelID, messageID string) (*discordgo.Message, error) {
			if session == nil {
				return nil, fmt.Errorf("discord session is nil")
			}
			return session.ChannelMessage(channelID, messageID)
		},
		store: newHTTPStateStore(jsonURL, &http.Client{Timeout: 10 * time.Second}),
	}, nil
}

func (h *Handler) onReactionAdd(session *discordgo.Session, reaction *discordgo.MessageReactionAdd) {
	if reaction != nil {
		h.onReactionChange(session, reaction.MessageReaction, 1)
	}
}

func (h *Handler) onReactionRemove(session *discordgo.Session, reaction *discordgo.MessageReactionRemove) {
	if reaction != nil {
		h.onReactionChange(session, reaction.MessageReaction, -1)
	}
}

func (h *Handler) onReactionChange(session *discordgo.Session, reaction *discordgo.MessageReaction, delta int) {
	if reaction == nil || !isWaffleEmoji(reaction.Emoji) {
		return
	}
	// Only count reactions in the channel that receives the ranking.
	if h.announcementChannelID == "" || reaction.ChannelID != h.announcementChannelID {
		return
	}

	message, err := h.loadMessage(session, reaction.ChannelID, reaction.MessageID)
	if err != nil {
		h.logger.Print("[waffle] 오류: 메시지 조회 실패")
		return
	}
	if message == nil || message.Author == nil {
		h.logger.Print("[waffle] 오류: 메시지 작성자 확인 실패")
		return
	}

	_, err = h.updateRecipient(message.Author.ID, delta)
	if err != nil {
		h.logger.Print("[waffle] 오류: 집계 갱신 실패")
		return
	}
}

func (h *Handler) updateRecipient(userID string, delta int) (int, error) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()

	state, err := h.store.Load(context.Background())
	if err != nil {
		return 0, fmt.Errorf("load state: %w", err)
	}
	if state.Version != stateVersion {
		return 0, fmt.Errorf("unsupported state version %d", state.Version)
	}
	if state.Recipients == nil {
		state.Recipients = make(map[string]int)
	}

	count := max(0, state.Recipients[userID]+delta)
	if count == 0 {
		delete(state.Recipients, userID)
	} else {
		state.Recipients[userID] = count
	}

	if err := h.store.Save(context.Background(), state); err != nil {
		return 0, fmt.Errorf("save state: %w", err)
	}

	return count, nil
}

func readJSONURL(path string) (string, error) {
	value, err := readMountedValue(path)
	if err != nil {
		return "", fmt.Errorf("read OCI waffle JSON URL: %w", err)
	}

	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("OCI waffle JSON URL file %q does not contain a valid HTTPS URL", path)
	}

	return value, nil
}

func readMountedValue(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", path, err)
	}

	value := strings.TrimSpace(string(contents))
	if value == "" {
		return "", fmt.Errorf("file %q is empty", path)
	}

	return value, nil
}

func isWaffleEmoji(emoji discordgo.Emoji) bool {
	return emoji.Name == customEmojiName || emoji.Name == unicodeEmoji
}

func (h *Handler) Register(session *discordgo.Session) {
	h.sendMessage = func(channelID, content string) error {
		_, err := session.ChannelMessageSend(channelID, content)
		return err
	}
	session.AddHandler(h.onReactionAdd)
	session.AddHandler(h.onReactionRemove)
}
