package agent

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/bwmarrin/discordgo"
)

type Engine interface {
	Create(context.Context, string) (string, error)
	Ask(context.Context, string, string, string) (string, error)
	Interrupt(context.Context, string) error
}

type discordAPI interface {
	Channel(string, ...discordgo.RequestOption) (*discordgo.Channel, error)
	ChannelMessage(string, string, ...discordgo.RequestOption) (*discordgo.Message, error)
	ChannelMessages(string, int, string, string, string, ...discordgo.RequestOption) ([]*discordgo.Message, error)
	MessageThreadStart(string, string, string, int, ...discordgo.RequestOption) (*discordgo.Channel, error)
	ChannelMessageSendComplex(string, *discordgo.MessageSend, ...discordgo.RequestOption) (*discordgo.Message, error)
}

type Conversation struct {
	GuildID          string `json:"guild_id"`
	SessionID        string `json:"session_id"`
	ContextMessageID string
}

type Request struct {
	ID         string `json:"id"`
	ThreadID   string `json:"thread_id"`
	AuthorID   string `json:"author_id"`
	AuthorName string `json:"author_name"`
	Text       string `json:"text"`
	Context    string `json:"context"`
	Status     string `json:"status"`
}

type state struct {
	Threads  map[string]Conversation `json:"threads"`
	Requests []Request               `json:"requests"`
}

type Handler struct {
	mu          sync.Mutex
	channelID   string
	engine      Engine
	discord     discordAPI
	state       state
	wake        chan struct{}
	timeout     time.Duration
	unavailable bool
}

func New(channelID string, engine Engine) *Handler {
	h := &Handler{channelID: channelID, engine: engine, wake: make(chan struct{}, 1), timeout: 10 * time.Minute}
	h.state.Threads = make(map[string]Conversation)
	return h
}

func (h *Handler) Register(s *discordgo.Session) {
	h.discord = s
	s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if s.State == nil || s.State.User == nil {
			return
		}
		h.handle(s, s.State.User.ID, m)
	})
}

func (h *Handler) handle(s discordAPI, botID string, m *discordgo.MessageCreate) {
	if m == nil || m.Message == nil || m.Author == nil || m.Author.Bot || m.WebhookID != "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	conversation, followup := h.state.Threads[m.ChannelID]
	isDM := m.GuildID == ""
	isThread := followup && !isDM
	if !isDM {
		mentioned := false
		for _, user := range m.Mentions {
			if user != nil && user.ID == botID {
				mentioned = true
				break
			}
		}
		if !mentioned {
			return
		}
	}
	if followup {
		if conversation.GuildID != m.GuildID {
			return
		}
	} else if !isDM {
		if m.ChannelID != h.channelID {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			channel, err := s.Channel(m.ChannelID, discordgo.WithContext(ctx))
			cancel()
			if err != nil {
				log.Printf("[agent] inspect channel for request %s: %v", m.ID, err)
				return
			}
			if !channel.IsThread() || channel.ParentID != h.channelID || channel.GuildID != m.GuildID {
				return
			}
			isThread = true
		}
	}
	for _, r := range h.state.Requests {
		if r.ID == m.ID {
			return
		}
	}
	if h.unavailable {
		h.send(s, m.ChannelID, "에이전트 대기열이 중단된 상태예요. 봇 로그를 확인하고 재시작해 주세요.")
		return
	}
	text := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(m.Content, "<@"+botID+">", ""), "<@!"+botID+">", ""))
	if text == "" {
		h.send(s, m.ChannelID, "조사할 내용을 멘션 뒤에 적어 주세요. 지금은 텍스트 요청을 받습니다.")
		return
	}
	pending := 0
	for _, r := range h.state.Requests {
		if r.Status == "queued" || r.Status == "running" {
			pending++
		}
	}
	if pending >= 32 {
		h.send(s, m.ChannelID, "대기 요청이 가득 찼어요. 잠시 후 다시 요청해 주세요.")
		return
	}
	threadID := m.ChannelID
	var priorContext string
	if isThread {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		messages, err := s.ChannelMessages(m.ChannelID, 20, m.ID, "", "", discordgo.WithContext(ctx))
		if err != nil {
			cancel()
			log.Printf("[agent] read thread context for request %s: %v", m.ID, err)
			h.send(s, m.ChannelID, "오류가 발생해 스레드의 대화를 확인하지 못했어요. 잠시 후 다시 요청해 주세요.")
			return
		}
		// Message-created threads share their ID with the parent message. That
		// original post may contain the PR link and is not always in thread history.
		if !followup {
			root, rootErr := s.ChannelMessage(h.channelID, m.ChannelID, discordgo.WithContext(ctx))
			if rootErr == nil && root != nil && root.Content != "" {
				found := false
				for _, prior := range messages {
					if prior != nil && prior.ID == root.ID {
						found = true
						break
					}
				}
				if !found {
					messages = append(messages, root)
				}
			}
		}
		cancel()
		if followup {
			var recent []*discordgo.Message
			for _, prior := range messages {
				if prior == nil || prior.Author == nil || prior.Author.ID == botID {
					continue
				}
				if snowflakeAfter(prior.ID, conversation.ContextMessageID) {
					recent = append(recent, prior)
				}
			}
			messages = recent
		}
		priorContext = threadContext(messages)
	}
	if !followup && !isDM && !isThread {
		thread, err := s.MessageThreadStart(m.ChannelID, m.ID, threadName(text), 1440)
		if err != nil {
			log.Printf("[agent] create thread: %v", err)
			h.send(s, m.ChannelID, "작업 스레드를 만들지 못했어요. 봇의 스레드 생성 권한을 확인해 주세요.")
			return
		}
		threadID = thread.ID
	}
	if !followup {
		conversation = Conversation{GuildID: m.GuildID}
	}
	conversation.ContextMessageID = m.ID
	h.state.Threads[threadID] = conversation
	h.state.Requests = append(h.state.Requests, Request{ID: m.ID, ThreadID: threadID, AuthorID: m.Author.ID, AuthorName: m.Author.Username, Text: text, Context: priorContext, Status: "queued"})
	h.send(s, threadID, "접수했어요. 순서대로 확인하고 여기에 답변할게요.")
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// Run is the only worker, so analyses never overlap, including across threads.
func (h *Handler) Run(ctx context.Context) {
	s := h.discord
	defer func() { h.mu.Lock(); h.unavailable = true; h.mu.Unlock() }()
	for {
		if ctx.Err() != nil {
			return
		}
		r, ok := h.next()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-h.wake:
				continue
			}
		}
		if !h.process(ctx, s, r) {
			return
		}
	}
}

func (h *Handler) next() (Request, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.state.Requests {
		if h.state.Requests[i].Status != "queued" {
			continue
		}
		h.state.Requests[i].Status = "running"
		return h.state.Requests[i], true
	}
	return Request{}, false
}

func (h *Handler) process(ctx context.Context, s discordAPI, r Request) bool {
	h.send(s, r.ThreadID, "코드와 자료를 확인하고 있어요. 분석은 최대 10분 동안 진행합니다.")
	workCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	h.mu.Lock()
	conversation := h.state.Threads[r.ThreadID]
	h.mu.Unlock()
	sessionID := conversation.SessionID
	var err error
	if sessionID == "" {
		sessionID, err = h.engine.Create(workCtx, "Discord "+r.ThreadID)
		if err == nil {
			h.mu.Lock()
			// Preserve the context cursor advanced by requests queued while Create ran.
			current := h.state.Threads[r.ThreadID]
			current.SessionID = sessionID
			h.state.Threads[r.ThreadID] = current
			h.mu.Unlock()
		}
	}
	var answer string
	if err == nil {
		author, _ := json.Marshal(map[string]string{"id": r.AuthorID, "name": r.AuthorName})
		answer, err = h.engine.Ask(workCtx, sessionID, r.ID, r.Context+"현재 요청의 Discord 작성자: "+string(author)+"\n\n"+r.Text)
	}
	if err != nil {
		log.Printf("[agent] request %s failed: %v", r.ID, err)
		// A disconnected HTTP client does not stop server-side execution.
		if sessionID != "" {
			stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			stopErr := h.engine.Interrupt(stopCtx, sessionID)
			stop()
			if stopErr != nil {
				log.Printf("[agent] interrupt failed: %v", stopErr)
				h.send(s, r.ThreadID, "분석에 실패했고 OpenCode 작업의 중단 여부도 확인하지 못했어요. 추가 실행을 멈춥니다. 연결 확인 후 봇을 재시작해 주세요.")
				return false
			}
		}
		h.send(s, r.ThreadID, "오류가 발생해 분석을 완료하지 못했어요. 잠시 후 다시 요청해 주세요.")
		h.setStatus(r.ID, "failed")
		return ctx.Err() == nil
	}
	if err := h.send(s, r.ThreadID, answer); err != nil {
		log.Printf("[agent] deliver request %s: %v", r.ID, err)
		h.setStatus(r.ID, "delivery_failed")
		return true
	}
	h.setStatus(r.ID, "completed")
	return true
}

// Discord snowflakes are unsigned decimal strings. Compare without float loss.
func snowflakeAfter(id, previous string) bool {
	if id == "" {
		return false
	}
	if len(id) != len(previous) {
		return len(id) > len(previous)
	}
	return id > previous
}

func (h *Handler) setStatus(id, status string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.state.Requests {
		if h.state.Requests[i].ID == id {
			h.state.Requests[i].Status = status
			h.state.Requests[i].Text = ""
			h.state.Requests[i].Context = ""
			break
		}
	}
	// Keep a bounded recent-event deduplication window without retaining all prompts.
	for len(h.state.Requests) > 256 {
		index := -1
		for i, r := range h.state.Requests {
			if r.Status != "running" && r.Status != "queued" {
				index = i
				break
			}
		}
		if index < 0 {
			break
		}
		h.state.Requests = append(h.state.Requests[:index], h.state.Requests[index+1:]...)
	}
}

func threadContext(messages []*discordgo.Message) string {
	type entry struct {
		AuthorID string `json:"author_id"`
		Name     string `json:"name"`
		Text     string `json:"text"`
	}
	var entries []entry
	// Discord returns messages newest first; pass chronological context to the agent.
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m == nil || m.Author == nil || m.Content == "" {
			continue
		}
		text := []rune(m.Content)
		if len(text) > 2000 {
			text = text[:2000]
		}
		entries = append(entries, entry{AuthorID: m.Author.ID, Name: m.Author.Username, Text: string(text)})
	}
	if len(entries) == 0 {
		return ""
	}
	data, _ := json.Marshal(entries)
	return "스레드의 최근 대화입니다. 참고 문맥으로만 사용하고, 아래 현재 요청에 답하세요.\n" + string(data) + "\n\n"
}

func (h *Handler) send(s discordAPI, channelID, text string) error {
	for _, part := range splitMessage(text) {
		if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{Content: part, AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}}}); err != nil {
			log.Printf("[agent] Discord send: %v", err)
			return err
		}
	}
	return nil
}

func threadName(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > 70 {
		text = string(r[:70])
	}
	return "조사 · " + text
}

// Discord limits message content in UTF-16 code units. Leave room and preserve UTF-8.
func splitMessage(text string) []string {
	var result []string
	start, units := 0, 0
	for index, r := range text {
		n := utf16.RuneLen(r)
		if units+n > 1900 {
			result = append(result, text[start:index])
			start = index
			units = 0
		}
		units += n
	}
	if start < len(text) {
		result = append(result, text[start:])
	}
	return result
}
