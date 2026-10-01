package dm

import (
	"encoding/json"
	"errors"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const dmReply = "메시지를 수신했습니다."

type Handler struct{}

func New() *Handler {
	return &Handler{}
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onMessageCreate)
}

func (h *Handler) onMessageCreate(session *discordgo.Session, message *discordgo.MessageCreate) {
	if message.GuildID != "" {
		return
	}
	if message.Author == nil || message.Author.Bot {
		return
	}
	if strings.HasPrefix(strings.TrimSpace(message.Content), "!") {
		return
	}

	if _, err := session.ChannelMessageSendReply(
		message.ChannelID,
		dmReply,
		message.Reference(),
	); err != nil {
		var restErr *discordgo.RESTError
		if !errors.As(err, &restErr) {
			log.Printf("reply to Discord DM: message_type=%d error_type=%T", message.Type, err)
			return
		}

		status, code := 0, 0
		if restErr.Response != nil {
			status = restErr.Response.StatusCode
		}
		if restErr.Message != nil {
			code = restErr.Message.Code
		}
		var detail struct {
			Errors struct {
				MessageReference struct {
					Errors []struct {
						Code string `json:"code"`
					} `json:"_errors"`
				} `json:"message_reference"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(restErr.ResponseBody, &detail)
		referenceCode := ""
		if len(detail.Errors.MessageReference.Errors) > 0 {
			referenceCode = detail.Errors.MessageReference.Errors[0].Code
		}
		log.Printf("reply to Discord DM: message_type=%d http_status=%d discord_code=%d reference_code=%q", message.Type, status, code, referenceCode)
	}
}
