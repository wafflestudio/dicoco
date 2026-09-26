package box

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	command        = "!박스"
	usersID        = "reference_box_users"
	numberID       = "reference_box_number"
	confirmID      = "reference_box_confirm"
	numberModalID  = "reference_box_number_modal:"
	interactionTTL = 30 * time.Minute
)

type boxState struct {
	ownerID   string
	channelID string
	messageID string
	number    int
	users     []string
	created   time.Time
}

type Handler struct {
	mu    sync.Mutex
	boxes map[string]boxState
	now   func() time.Time
}

func New() *Handler {
	return &Handler{
		boxes: make(map[string]boxState),
		now:   time.Now,
	}
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onMessageCreate)
	session.AddHandler(h.onInteractionCreate)
}

func (h *Handler) onMessageCreate(session *discordgo.Session, event *discordgo.MessageCreate) {
	if event == nil || event.Message == nil || event.Author == nil || event.Author.Bot || event.GuildID != "" {
		return
	}
	if strings.TrimSpace(event.Content) != command {
		return
	}

	state := boxState{
		ownerID:   event.Author.ID,
		channelID: event.ChannelID,
		created:   h.now(),
	}
	message, err := session.ChannelMessageSendComplex(event.ChannelID, &discordgo.MessageSend{
		Content:         render(state),
		Components:      components(state),
		Reference:       event.Reference(),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
	if err != nil {
		log.Printf("reference box: send: %v", err)
		return
	}

	state.messageID = message.ID
	h.mu.Lock()
	h.removeExpiredLocked()
	h.boxes[message.ID] = state
	h.mu.Unlock()
}

func (h *Handler) onInteractionCreate(session *discordgo.Session, event *discordgo.InteractionCreate) {
	if event == nil || event.Interaction == nil {
		return
	}
	switch event.Type {
	case discordgo.InteractionMessageComponent:
		h.onComponent(session, event)
	case discordgo.InteractionModalSubmit:
		h.onModalSubmit(session, event)
	}
}

func (h *Handler) onComponent(session *discordgo.Session, event *discordgo.InteractionCreate) {
	data := event.MessageComponentData()
	if data.CustomID != usersID && data.CustomID != numberID && data.CustomID != confirmID {
		return
	}
	if event.Message == nil {
		respondEphemeral(session, event.Interaction, "박스 메시지를 확인할 수 없어요.")
		return
	}

	state, ok := h.get(event.Message.ID)
	if !ok {
		respondEphemeral(session, event.Interaction, "이 박스는 만료됐어요. `!박스`로 새로 만들어 주세요.")
		return
	}
	if interactionUserID(event.Interaction) != state.ownerID {
		respondEphemeral(session, event.Interaction, "이 박스는 명령어를 입력한 사람만 사용할 수 있어요.")
		return
	}

	switch data.CustomID {
	case usersID:
		state.users = append([]string(nil), data.Values...)
		h.put(state)
		if err := session.InteractionRespond(event.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:         render(state),
				Components:      components(state),
				AllowedMentions: &discordgo.MessageAllowedMentions{},
			},
		}); err != nil {
			log.Printf("reference box: update users: %v", err)
		}
	case numberID:
		value := ""
		if state.number != 0 {
			value = strconv.Itoa(state.number)
		}
		if err := session.InteractionRespond(event.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: &discordgo.InteractionResponseData{
				CustomID: numberModalID + state.messageID,
				Title:    "숫자 입력",
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{
							discordgo.TextInput{
								CustomID:    numberID,
								Label:       "숫자",
								Style:       discordgo.TextInputShort,
								Placeholder: "1 이상의 정수를 입력해 주세요",
								Value:       value,
								Required:    true,
								MinLength:   1,
								MaxLength:   10,
							},
						},
					},
				},
			},
		}); err != nil {
			log.Printf("reference box: open number modal: %v", err)
		}
	case confirmID:
		respond(session, event.Interaction, result(state))
	}
}

func (h *Handler) onModalSubmit(session *discordgo.Session, event *discordgo.InteractionCreate) {
	data := event.ModalSubmitData()
	if !strings.HasPrefix(data.CustomID, numberModalID) {
		return
	}
	messageID := strings.TrimPrefix(data.CustomID, numberModalID)
	state, ok := h.get(messageID)
	if !ok {
		respondEphemeral(session, event.Interaction, "이 박스는 만료됐어요. `!박스`로 새로 만들어 주세요.")
		return
	}
	if interactionUserID(event.Interaction) != state.ownerID {
		respondEphemeral(session, event.Interaction, "이 박스는 명령어를 입력한 사람만 사용할 수 있어요.")
		return
	}

	value, ok := modalTextValue(data.Components, numberID)
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if !ok || err != nil || number <= 0 {
		respondEphemeral(session, event.Interaction, "숫자는 1 이상의 정수로 입력해 주세요.")
		return
	}
	state.number = number
	h.put(state)
	respondEphemeral(session, event.Interaction, fmt.Sprintf("숫자 `%d`을(를) 받았어요.", number))

	content := render(state)
	componentList := components(state)
	if _, err := session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:              state.messageID,
		Channel:         state.channelID,
		Content:         &content,
		Components:      &componentList,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Printf("reference box: update number: %v", err)
	}
}

func (h *Handler) get(messageID string) (boxState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, ok := h.boxes[messageID]
	if ok && h.now().Sub(state.created) > interactionTTL {
		delete(h.boxes, messageID)
		return boxState{}, false
	}
	return state, ok
}

func (h *Handler) put(state boxState) {
	h.mu.Lock()
	h.boxes[state.messageID] = state
	h.mu.Unlock()
}

func (h *Handler) removeExpiredLocked() {
	now := h.now()
	for id, state := range h.boxes {
		if now.Sub(state.created) > interactionTTL {
			delete(h.boxes, id)
		}
	}
}

func components(state boxState) []discordgo.MessageComponent {
	zero := 0
	defaults := make([]discordgo.SelectMenuDefaultValue, 0, len(state.users))
	for _, id := range state.users {
		defaults = append(defaults, discordgo.SelectMenuDefaultValue{ID: id, Type: discordgo.SelectMenuDefaultValueUser})
	}
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.SelectMenu{
			MenuType:      discordgo.UserSelectMenu,
			CustomID:      usersID,
			Placeholder:   "사람을 찾아 선택해 주세요",
			MinValues:     &zero,
			MaxValues:     25,
			DefaultValues: defaults,
		}}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{CustomID: numberID, Label: "숫자 입력", Style: discordgo.SecondaryButton},
			discordgo.Button{CustomID: confirmID, Label: "확인", Style: discordgo.SuccessButton},
		}},
	}
}

func render(state boxState) string {
	number := "미입력"
	if state.number > 0 {
		number = strconv.Itoa(state.number)
	}
	users := "선택 없음"
	if len(state.users) > 0 {
		mentions := make([]string, 0, len(state.users))
		for _, id := range state.users {
			mentions = append(mentions, "<@"+id+">")
		}
		users = strings.Join(mentions, ", ")
	}
	return fmt.Sprintf("📦 **테스트 박스**\n숫자: %s\n사람: %s\n\n사람을 선택하고 숫자를 입력한 뒤 **확인**을 눌러 주세요.", number, users)
}

func result(state boxState) string {
	number := "미입력"
	if state.number > 0 {
		number = strconv.Itoa(state.number)
	}
	users := "선택 없음"
	if len(state.users) > 0 {
		mentions := make([]string, 0, len(state.users))
		for _, id := range state.users {
			mentions = append(mentions, "<@"+id+">")
		}
		users = strings.Join(mentions, ", ")
	}
	return fmt.Sprintf("받은 값\n숫자: %s\n사람: %s", number, users)
}

func modalTextValue(components []discordgo.MessageComponent, customID string) (string, bool) {
	for _, component := range components {
		row, ok := component.(*discordgo.ActionsRow)
		if !ok {
			if value, valueOK := component.(discordgo.ActionsRow); valueOK {
				row = &value
				ok = true
			}
		}
		if !ok {
			continue
		}
		for _, child := range row.Components {
			input, ok := child.(*discordgo.TextInput)
			if ok && input.CustomID == customID {
				return input.Value, true
			}
			if value, valueOK := child.(discordgo.TextInput); valueOK && value.CustomID == customID {
				return value.Value, true
			}
		}
	}
	return "", false
}

func interactionUserID(interaction *discordgo.Interaction) string {
	if interaction.Member != nil && interaction.Member.User != nil {
		return interaction.Member.User.ID
	}
	if interaction.User != nil {
		return interaction.User.ID
	}
	return ""
}

func respond(session *discordgo.Session, interaction *discordgo.Interaction, content string) {
	if err := session.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         content,
			Flags:           discordgo.MessageFlagsEphemeral,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	}); err != nil {
		log.Printf("reference box: respond: %v", err)
	}
}

func respondEphemeral(session *discordgo.Session, interaction *discordgo.Interaction, content string) {
	respond(session, interaction, content)
}
