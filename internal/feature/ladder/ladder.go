package ladder

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/wafflestudio/dicoco/internal/feature/scratch"
)

const (
	command         = "!박스"
	usage           = "사용법: `!박스 @역할`"
	usersPrefix     = "ladder_excluded_"
	previousID      = "ladder_page_previous"
	nextID          = "ladder_page_next"
	numberID        = "ladder_number"
	confirmID       = "ladder_confirm"
	numberModalID   = "ladder_number_modal:"
	interactionTTL  = 30 * time.Minute
	optionsPerPage  = 25
	maxMessageRunes = 1900
)

type boxState struct {
	ownerID    string
	guildID    string
	channelID  string
	messageID  string
	roleID     string
	number     int
	page       int
	candidates []candidate
	excluded   []string
	created    time.Time
	running    bool
	completed  bool
}

type candidate struct {
	id    string
	label string
}

type rankedUser struct {
	candidate candidate
	result    scratch.Result
}

type Handler struct {
	mu             sync.Mutex
	adminChannelID string
	voiceChannelID string
	boxes          map[string]boxState
	now            func() time.Time
	run            func() (scratch.Result, error)
}

func New(run func() (scratch.Result, error)) *Handler {
	read := func(name string) string {
		data, err := os.ReadFile("/var/run/secrets/discord-bot/" + name)
		if err != nil {
			log.Printf("ladder: read %s: %v", name, err)
		}
		return strings.TrimSpace(string(data))
	}
	return &Handler{
		adminChannelID: read("admin_2026_channel_id"),
		voiceChannelID: read("admin_voice_channel_id"),
		boxes:          make(map[string]boxState),
		now:            time.Now,
		run:            run,
	}
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onMessageCreate)
	session.AddHandler(h.onInteractionCreate)
}

func (h *Handler) onMessageCreate(session *discordgo.Session, event *discordgo.MessageCreate) {
	if event == nil || event.Message == nil || event.Author == nil || event.Author.Bot || event.GuildID == "" {
		return
	}
	if event.ChannelID != h.adminChannelID && event.ChannelID != h.voiceChannelID {
		return
	}
	fields := strings.Fields(event.Content)
	if len(fields) == 0 || fields[0] != command {
		return
	}
	roleID, ok := roleMention(fields)
	if !ok {
		h.sendReply(session, event.ChannelID, event.Reference(), usage)
		return
	}
	if err := validateRole(session, event.GuildID, roleID); err != nil {
		h.sendReply(session, event.ChannelID, event.Reference(), err.Error())
		return
	}
	candidates, err := roleCandidates(session, event.GuildID, roleID)
	if err != nil {
		h.sendReply(session, event.ChannelID, event.Reference(), err.Error())
		return
	}
	if len(candidates) == 0 {
		h.sendReply(session, event.ChannelID, event.Reference(), "지정한 역할을 가진 참가자가 없어요.")
		return
	}

	state := boxState{
		ownerID:    event.Author.ID,
		guildID:    event.GuildID,
		channelID:  event.ChannelID,
		roleID:     roleID,
		candidates: candidates,
		created:    h.now(),
	}
	message, err := session.ChannelMessageSendComplex(event.ChannelID, &discordgo.MessageSend{
		Content:         render(state),
		Components:      components(state, false),
		Reference:       event.Reference(),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
	if err != nil {
		log.Printf("ladder: send box: %v", err)
		return
	}

	state.messageID = message.ID
	h.mu.Lock()
	h.removeExpiredLocked()
	h.boxes[message.ID] = state
	h.mu.Unlock()
}

func roleMention(fields []string) (string, bool) {
	if len(fields) != 2 || fields[0] != command || !strings.HasPrefix(fields[1], "<@&") || !strings.HasSuffix(fields[1], ">") {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(fields[1], "<@&"), ">")
	if id == "" {
		return "", false
	}
	for _, digit := range id {
		if digit < '0' || digit > '9' {
			return "", false
		}
	}
	return id, true
}

func validateRole(session *discordgo.Session, guildID, roleID string) error {
	if session.State != nil {
		if _, err := session.State.Role(guildID, roleID); err == nil {
			return nil
		}
	}
	roles, err := session.GuildRoles(guildID)
	if err != nil {
		log.Printf("ladder: get guild roles: %v", err)
		return fmt.Errorf("역할 정보를 확인하지 못했어요. 잠시 후 다시 시도해 주세요.")
	}
	for _, role := range roles {
		if role != nil && role.ID == roleID {
			return nil
		}
	}
	return fmt.Errorf("이 서버에서 지정한 역할을 찾지 못했어요.\n%s", usage)
}

func roleCandidates(session *discordgo.Session, guildID, roleID string) ([]candidate, error) {
	var candidates []candidate
	for after := ""; ; {
		members, err := session.GuildMembers(guildID, after, 1000)
		if err != nil {
			log.Printf("ladder: list guild members: %v", err)
			return nil, fmt.Errorf("역할 구성원을 조회하지 못했어요. Discord Developer Portal의 Server Members Intent 설정을 확인해 주세요.")
		}
		candidates = append(candidates, candidatesWithRole(members, roleID)...)
		if len(members) < 1000 {
			return candidates, nil
		}
		last := members[len(members)-1]
		if last == nil || last.User == nil || last.User.ID == "" || last.User.ID == after {
			return nil, fmt.Errorf("역할 구성원 목록을 끝까지 확인하지 못했어요. 잠시 후 다시 시도해 주세요.")
		}
		after = last.User.ID
	}
}

func candidatesWithRole(members []*discordgo.Member, roleID string) []candidate {
	var candidates []candidate
	for _, member := range members {
		if member != nil && member.User != nil && member.User.ID != "" && !member.User.Bot && contains(member.Roles, roleID) {
			candidates = append(candidates, candidateFromUser(member.User))
		}
	}
	return candidates
}

func candidateFromUser(user *discordgo.User) candidate {
	label := user.GlobalName
	if label == "" {
		label = user.Username
	}
	if label == "" {
		label = user.ID
	}
	return candidate{id: user.ID, label: label}
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
	page, isUsersMenu := usersPage(data.CustomID)
	if !isUsersMenu && data.CustomID != previousID && data.CustomID != nextID && data.CustomID != numberID && data.CustomID != confirmID {
		return
	}
	if event.Message == nil {
		respondEphemeral(session, event.Interaction, "추첨 박스 메시지를 확인할 수 없어요.")
		return
	}
	messageID := event.Message.ID
	userID := interactionUserID(event.Interaction)

	switch {
	case isUsersMenu:
		state, message := h.setExcluded(messageID, userID, page, data.Values)
		if message != "" {
			respondEphemeral(session, event.Interaction, message)
			return
		}
		h.respondUpdate(session, event.Interaction, state)
	case data.CustomID == previousID || data.CustomID == nextID:
		delta := -1
		if data.CustomID == nextID {
			delta = 1
		}
		state, message := h.movePage(messageID, userID, delta)
		if message != "" {
			respondEphemeral(session, event.Interaction, message)
			return
		}
		h.respondUpdate(session, event.Interaction, state)
	case data.CustomID == numberID:
		state, message := h.editableState(messageID, userID)
		if message != "" {
			respondEphemeral(session, event.Interaction, message)
			return
		}
		value := ""
		if state.number > 0 {
			value = strconv.Itoa(state.number)
		}
		if err := session.InteractionRespond(event.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: &discordgo.InteractionResponseData{
				CustomID: numberModalID + state.messageID,
				Title:    "뽑을 인원 입력",
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    numberID,
							Label:       "뽑을 인원",
							Style:       discordgo.TextInputShort,
							Placeholder: "1 이상의 정수를 입력해 주세요",
							Value:       value,
							Required:    true,
							MinLength:   1,
							MaxLength:   10,
						},
					}},
				},
			},
		}); err != nil {
			log.Printf("ladder: open number modal: %v", err)
		}
	case data.CustomID == confirmID:
		state, message := h.begin(messageID, userID)
		if message != "" {
			respondEphemeral(session, event.Interaction, message)
			return
		}
		if err := session.InteractionRespond(event.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		}); err != nil {
			h.finish(messageID, false)
			log.Printf("ladder: defer draw: %v", err)
			return
		}
		h.editBox(session, state, "⏳ 참가자별 10,000회 채굴을 진행하고 있어요.", components(state, true))
		go h.draw(session, state)
	}
}

func (h *Handler) onModalSubmit(session *discordgo.Session, event *discordgo.InteractionCreate) {
	data := event.ModalSubmitData()
	if !strings.HasPrefix(data.CustomID, numberModalID) {
		return
	}
	messageID := strings.TrimPrefix(data.CustomID, numberModalID)
	value, ok := modalTextValue(data.Components, numberID)
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if !ok || err != nil || number <= 0 {
		respondEphemeral(session, event.Interaction, "뽑을 인원은 1 이상의 정수로 입력해 주세요.")
		return
	}
	state, message := h.setNumber(messageID, interactionUserID(event.Interaction), number)
	if message != "" {
		respondEphemeral(session, event.Interaction, message)
		return
	}
	respondEphemeral(session, event.Interaction, fmt.Sprintf("뽑을 인원 `%d`명을 설정했어요.", number))
	h.editBox(session, state, "", components(state, false))
}

func (h *Handler) respondUpdate(session *discordgo.Session, interaction *discordgo.Interaction, state boxState) {
	if err := session.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:         render(state),
			Components:      components(state, false),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	}); err != nil {
		log.Printf("ladder: update box: %v", err)
	}
}

func (h *Handler) editableState(messageID, userID string) (boxState, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, message := h.editableStateLocked(messageID, userID)
	return cloneState(state), message
}

func (h *Handler) setExcluded(messageID, userID string, page int, selected []string) (boxState, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, message := h.editableStateLocked(messageID, userID)
	if message != "" {
		return boxState{}, message
	}
	if page != state.page {
		return boxState{}, "참가자 목록이 바뀌었어요. 현재 페이지에서 다시 선택해 주세요."
	}
	start := page * optionsPerPage
	end := min(start+optionsPerPage, len(state.candidates))
	pageIDs := make(map[string]bool, end-start)
	for _, person := range state.candidates[start:end] {
		pageIDs[person.id] = true
	}
	selectedOnPage := make(map[string]bool, len(selected))
	for _, id := range selected {
		if !pageIDs[id] {
			return boxState{}, "참가자 목록을 확인하지 못했어요. 다시 선택해 주세요."
		}
		selectedOnPage[id] = true
	}
	var excluded []string
	for _, id := range state.excluded {
		if !pageIDs[id] {
			excluded = append(excluded, id)
		}
	}
	for _, person := range state.candidates[start:end] {
		if selectedOnPage[person.id] {
			excluded = append(excluded, person.id)
		}
	}
	state.excluded = excluded
	h.boxes[messageID] = cloneState(state)
	return cloneState(state), ""
}

func (h *Handler) movePage(messageID, userID string, delta int) (boxState, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, message := h.editableStateLocked(messageID, userID)
	if message != "" {
		return boxState{}, message
	}
	lastPage := (len(state.candidates) - 1) / optionsPerPage
	page := state.page + delta
	if page < 0 || page > lastPage {
		return boxState{}, "이동할 참가자 페이지가 없어요."
	}
	state.page = page
	h.boxes[messageID] = cloneState(state)
	return cloneState(state), ""
}

func (h *Handler) setNumber(messageID, userID string, number int) (boxState, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, message := h.editableStateLocked(messageID, userID)
	if message != "" {
		return boxState{}, message
	}
	state.number = number
	h.boxes[messageID] = cloneState(state)
	return cloneState(state), ""
}

func (h *Handler) begin(messageID, userID string) (boxState, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, message := h.editableStateLocked(messageID, userID)
	if message != "" {
		return boxState{}, message
	}
	eligible := len(state.candidates) - len(state.excluded)
	if state.number <= 0 {
		return boxState{}, "먼저 뽑을 인원을 입력해 주세요."
	}
	if eligible == 0 {
		return boxState{}, "추첨할 참가자가 없어요. 제외할 사람을 다시 선택해 주세요."
	}
	if state.number > eligible {
		return boxState{}, fmt.Sprintf("참가자는 %d명이에요. 뽑을 인원은 1~%d명으로 입력해 주세요.", eligible, eligible)
	}
	state.running = true
	h.boxes[messageID] = cloneState(state)
	return cloneState(state), ""
}

func (h *Handler) editableStateLocked(messageID, userID string) (boxState, string) {
	state, ok := h.boxes[messageID]
	if ok && h.now().Sub(state.created) > interactionTTL {
		delete(h.boxes, messageID)
		ok = false
	}
	if !ok {
		return boxState{}, "이 추첨 박스는 만료됐어요. `!박스`로 새로 만들어 주세요."
	}
	if userID != state.ownerID {
		return boxState{}, "이 박스는 명령어를 입력한 사람만 사용할 수 있어요."
	}
	if state.running {
		return boxState{}, "이미 추첨을 진행하고 있어요."
	}
	if state.completed {
		return boxState{}, "이 박스의 추첨은 이미 끝났어요."
	}
	return state, ""
}

func (h *Handler) finish(messageID string, completed bool) boxState {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, ok := h.boxes[messageID]
	if !ok {
		return boxState{}
	}
	state.running = false
	state.completed = completed
	h.boxes[messageID] = cloneState(state)
	return cloneState(state)
}

func (h *Handler) draw(session *discordgo.Session, state boxState) {
	excluded := make(map[string]bool, len(state.excluded))
	for _, id := range state.excluded {
		excluded[id] = true
	}
	participants := make([]candidate, 0, len(state.candidates)-len(state.excluded))
	for _, person := range state.candidates {
		if !excluded[person.id] {
			participants = append(participants, person)
		}
	}
	ranked, err := h.rank(participants, func(block rankedUser) {
		h.sendReply(session, state.channelID, &discordgo.MessageReference{
			MessageID: state.messageID,
			ChannelID: state.channelID,
			GuildID:   state.guildID,
		}, "<@"+block.candidate.id+">\n"+block.result.BlockMessage())
	})
	if err != nil {
		log.Printf("ladder: draw: %v", err)
		h.drawFailed(session, state, fmt.Errorf("참가자 점수 계산에 실패해 추첨을 중단했어요. 잠시 후 다시 확인해 주세요."))
		return
	}
	h.finish(state.messageID, true)
	h.publishResults(session, state, resultMessages(ranked, state.number, state.excluded))
}

func (h *Handler) rank(participants []candidate, onBlock func(rankedUser)) ([]rankedUser, error) {
	if h.run == nil {
		return nil, fmt.Errorf("채굴 기능을 사용할 수 없어요")
	}
	ranked := make([]rankedUser, 0, len(participants))
	for _, person := range participants {
		result, err := h.run()
		if result.Submission != "" {
			if onBlock != nil {
				onBlock(rankedUser{candidate: person, result: result})
			}
			if err != nil {
				log.Printf("ladder submission user_id=%s: %v", person.id, err)
			}
		} else if err != nil {
			return nil, fmt.Errorf("user_id=%s: %w", person.id, err)
		}
		ranked = append(ranked, rankedUser{candidate: person, result: result})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].result.Score != ranked[j].result.Score {
			return ranked[i].result.Score > ranked[j].result.Score
		}
		return scratch.HashString(ranked[i].result.BestHash) < scratch.HashString(ranked[j].result.BestHash)
	})

	return ranked, nil
}

func (h *Handler) drawFailed(session *discordgo.Session, state boxState, cause error) {
	updated := h.finish(state.messageID, false)
	if updated.messageID == "" {
		return
	}
	h.editBox(session, updated, cause.Error()+" 다시 시도할 수 있어요.\n\n"+render(updated), components(updated, false))
}

func (h *Handler) publishResults(session *discordgo.Session, state boxState, messages []string) {
	if len(messages) == 0 {
		return
	}
	components := []discordgo.MessageComponent{}
	if _, err := session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:              state.messageID,
		Channel:         state.channelID,
		Content:         &messages[0],
		Components:      &components,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Printf("ladder: publish result: %v", err)
		h.sendReply(session, state.channelID, &discordgo.MessageReference{
			MessageID: state.messageID,
			ChannelID: state.channelID,
			GuildID:   state.guildID,
		}, messages[0])
	}
	for _, content := range messages[1:] {
		h.sendReply(session, state.channelID, &discordgo.MessageReference{
			MessageID: state.messageID,
			ChannelID: state.channelID,
			GuildID:   state.guildID,
		}, content)
	}
}

func (h *Handler) editBox(session *discordgo.Session, state boxState, content string, controls []discordgo.MessageComponent) {
	if state.messageID == "" {
		return
	}
	if content == "" {
		content = render(state)
	}
	if _, err := session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:              state.messageID,
		Channel:         state.channelID,
		Content:         &content,
		Components:      &controls,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Printf("ladder: edit box: %v", err)
	}
}

func (h *Handler) sendReply(session *discordgo.Session, channelID string, reference *discordgo.MessageReference, content string) {
	if _, err := session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         content,
		Reference:       reference,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Printf("ladder: send reply: %v", err)
	}
}

func components(state boxState, disabled bool) []discordgo.MessageComponent {
	start := state.page * optionsPerPage
	end := min(start+optionsPerPage, len(state.candidates))
	zero := 0
	options := make([]discordgo.SelectMenuOption, 0, end-start)
	for _, person := range state.candidates[start:end] {
		options = append(options, discordgo.SelectMenuOption{
			Label:   person.label,
			Value:   person.id,
			Default: contains(state.excluded, person.id),
		})
	}
	menu := discordgo.SelectMenu{
		MenuType:    discordgo.StringSelectMenu,
		CustomID:    usersPrefix + strconv.Itoa(state.page),
		Placeholder: "제외할 사람 선택",
		MinValues:   &zero,
		MaxValues:   len(options),
		Options:     options,
		Disabled:    disabled,
	}
	lastPage := max(0, (len(state.candidates)-1)/optionsPerPage)
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{menu}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{CustomID: previousID, Label: "이전", Style: discordgo.SecondaryButton, Disabled: disabled || state.page == 0},
			discordgo.Button{CustomID: nextID, Label: "다음", Style: discordgo.SecondaryButton, Disabled: disabled || state.page >= lastPage},
		}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{CustomID: numberID, Label: "뽑을 인원 입력", Style: discordgo.SecondaryButton, Disabled: disabled},
			discordgo.Button{CustomID: confirmID, Label: "추첨 시작", Style: discordgo.SuccessButton, Disabled: disabled},
		}},
	}
}

func render(state boxState) string {
	number := "미입력"
	if state.number > 0 {
		number = strconv.Itoa(state.number) + "명"
	}
	pageCount := max(1, (len(state.candidates)+optionsPerPage-1)/optionsPerPage)
	return fmt.Sprintf("📦 **사다리 추첨**\n대상 역할: <@&%s>\n후보: %d명 · 제외: %d명\n뽑을 인원: %s\n참가자 목록: %d/%d 페이지\n\n제외할 사람을 선택하고 뽑을 인원을 입력한 뒤 **추첨 시작**을 눌러 주세요.", state.roleID, len(state.candidates), len(state.excluded), number, state.page+1, pageCount)
}

func resultMessages(ranked []rankedUser, number int, excluded []string) []string {
	var messages []string
	content := fmt.Sprintf("🪜 추첨 결과 · 후보 %d명 중 %d명 선정\n\n🏆 선정", len(ranked), number)
	appendPart := func(part string) {
		if len([]rune(content+part)) > maxMessageRunes {
			messages = append(messages, content)
			content = "🪜 추첨 결과 · 이어서"
		}
		content += part
	}
	tied := false
	for i, user := range ranked {
		part := fmt.Sprintf("\n%d. <@%s> — %d점 - `%s`", i+1, user.candidate.id, user.result.Score, scratch.HashString(user.result.BestHash)[:7])
		if i == number {
			part = "\n\n나머지 참가자" + part
		}
		appendPart(part)
		if i > 0 && ranked[i-1].result.Score == user.result.Score {
			tied = true
		}
	}
	if len(excluded) > 0 {
		excludedIDs := append([]string(nil), excluded...)
		sort.Strings(excludedIDs)
		appendPart(fmt.Sprintf("\n\n제외한 참가자 (%d명)", len(excludedIDs)))
		for _, id := range excludedIDs {
			appendPart("\n<@" + id + ">")
		}
	}
	if tied {
		appendPart("\n\n동점은 해시값으로 순위를 정했어요.")
	}
	return append(messages, content)
}

func usersPage(customID string) (int, bool) {
	if !strings.HasPrefix(customID, usersPrefix) {
		return 0, false
	}
	page, err := strconv.Atoi(strings.TrimPrefix(customID, usersPrefix))
	return page, err == nil && page >= 0
}

func modalTextValue(components []discordgo.MessageComponent, customID string) (string, bool) {
	for _, component := range components {
		var row *discordgo.ActionsRow
		switch value := component.(type) {
		case *discordgo.ActionsRow:
			row = value
		case discordgo.ActionsRow:
			row = &value
		}
		if row == nil {
			continue
		}
		for _, child := range row.Components {
			switch input := child.(type) {
			case *discordgo.TextInput:
				if input.CustomID == customID {
					return input.Value, true
				}
			case discordgo.TextInput:
				if input.CustomID == customID {
					return input.Value, true
				}
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

func respondEphemeral(session *discordgo.Session, interaction *discordgo.Interaction, content string) {
	if err := session.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         content,
			Flags:           discordgo.MessageFlagsEphemeral,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	}); err != nil {
		log.Printf("ladder: respond: %v", err)
	}
}

func cloneState(state boxState) boxState {
	state.candidates = append([]candidate(nil), state.candidates...)
	state.excluded = append([]string(nil), state.excluded...)
	return state
}

func (h *Handler) removeExpiredLocked() {
	now := h.now()
	for id, state := range h.boxes {
		if now.Sub(state.created) > interactionTTL {
			delete(h.boxes, id)
		}
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
