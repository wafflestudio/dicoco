package draw

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/wafflestudio/dicoco/internal/feature/scratch"
)

const usage = "사용법: `!사다리 3` 또는 `!사다리 @사람1 -@사람2 2`\n어드민 음성 채널 참가자에 @멘션은 추가하고 -@멘션은 제외해요. 추가와 제외에 모두 적으면 제외가 우선이에요."

type Handler struct {
	voiceChannelID string
	run            func() (scratch.Result, error)
}

func New(run func() (scratch.Result, error)) *Handler {
	read := func(name string) string {
		data, err := os.ReadFile("/var/run/secrets/discord-bot/" + name)
		if err != nil {
			log.Printf("draw: read %s: %v", name, err)
		}
		return strings.TrimSpace(string(data))
	}
	return &Handler{voiceChannelID: read("admin_voice_channel_id"), run: run}
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onMessageCreate)
}

func parse(message *discordgo.Message) (int, []*discordgo.User, map[string]bool, error) {
	fields := strings.Fields(message.Content)
	if len(fields) < 2 || fields[0] != "!사다리" {
		return 0, nil, nil, fmt.Errorf("%s", usage)
	}
	n, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil || n <= 0 {
		return 0, nil, nil, fmt.Errorf("뽑을 인원은 1 이상의 정수로 입력해 주세요.\n%s", usage)
	}
	var extra []*discordgo.User
	excluded := make(map[string]bool)
	for _, field := range fields[1 : len(fields)-1] {
		exclude := strings.HasPrefix(field, "-")
		if exclude {
			field = strings.TrimPrefix(field, "-")
		}
		var matched *discordgo.User
		for _, user := range message.Mentions {
			if user != nil && (field == user.Mention() || field == "<@!"+user.ID+">") {
				matched = user
				break
			}
		}
		if matched == nil {
			return 0, nil, nil, fmt.Errorf("사람은 Discord 멘션으로 지정해 주세요.\n%s", usage)
		}
		if exclude {
			excluded[matched.ID] = true
		} else {
			extra = append(extra, matched)
		}
	}
	return n, extra, excluded, nil
}

// Copy IDs under the state lock: Discord updates voice states concurrently.
func voiceIDs(state *discordgo.State, guildID, channelID string) ([]string, error) {
	state.RLock()
	defer state.RUnlock()
	for _, guild := range state.Guilds {
		if guild.ID == guildID {
			var ids []string
			for _, voice := range guild.VoiceStates {
				if voice != nil && voice.ChannelID == channelID {
					ids = append(ids, voice.UserID)
				}
			}
			return ids, nil
		}
	}
	return nil, fmt.Errorf("서버의 음성 참가자 정보를 아직 불러오지 못했어요. 잠시 후 다시 시도해 주세요.")
}

func candidates(voice, extra []*discordgo.User, excluded map[string]bool) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, group := range [][]*discordgo.User{voice, extra} {
		for _, user := range group {
			if user != nil && user.ID != "" && !user.Bot && !seen[user.ID] && !excluded[user.ID] {
				seen[user.ID] = true
				ids = append(ids, user.ID)
			}
		}
	}
	return ids
}

type rankedUser struct {
	id     string
	result scratch.Result
}

func (h *Handler) pick(ids []string, n int, onBlock func(string, scratch.Result)) ([]rankedUser, error) {
	if n <= 0 || n > len(ids) {
		return nil, fmt.Errorf("후보는 %d명이에요. 뽑을 인원은 1~%d명으로 입력해 주세요.", len(ids), len(ids))
	}
	ranked := make([]rankedUser, 0, len(ids))
	for _, id := range ids {
		result, err := h.run()
		if result.Submission != "" {
			if onBlock != nil {
				onBlock(id, result)
			}
			if err != nil {
				log.Printf("draw submission user_id=%s: %v", id, err)
			}
		} else if err != nil {
			log.Printf("draw mining user_id=%s: %v", id, err)
			return nil, fmt.Errorf("점수 계산에 실패해 사다리를 중단했어요. 잠시 후 다시 시도해 주세요.")
		}
		ranked = append(ranked, rankedUser{id: id, result: result})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].result.Score != ranked[j].result.Score {
			return ranked[i].result.Score > ranked[j].result.Score
		}
		// Lower Bitcoin hashes break ties in the rounded display score.
		return scratch.HashString(ranked[i].result.BestHash) < scratch.HashString(ranked[j].result.BestHash)
	})
	return ranked, nil
}

func resultMessages(ranked []rankedUser, n int, excluded map[string]bool) []string {
	var messages []string
	content := fmt.Sprintf("🪜 사다리 결과 · 후보 %d명 중 %d명 선정\n\n🎯 선정", len(ranked), n)
	appendPart := func(part string) {
		if len([]rune(content+part)) > 1900 {
			messages = append(messages, content)
			content = "🪜 사다리 결과 · 이어서"
		}
		content += part
	}
	tied := false
	for i, user := range ranked {
		part := fmt.Sprintf("\n%d. <@%s> — %d점", i+1, user.id, user.result.Score)
		if i == n {
			part = "\n\n나머지 참가자" + part
		}
		appendPart(part)
		if i > 0 && ranked[i-1].result.Score == user.result.Score {
			tied = true
		}
	}
	var excludedIDs []string
	for id, exclude := range excluded {
		if exclude {
			excludedIDs = append(excludedIDs, id)
		}
	}
	sort.Strings(excludedIDs)
	for i, id := range excludedIDs {
		prefix := " "
		if i == 0 {
			prefix = "\n\n제외: "
		}
		appendPart(prefix + "<@" + id + ">")
	}
	if tied {
		appendPart("\n\n동점은 해시값으로 순위를 정했어요.")
	}
	return append(messages, content)
}

func (h *Handler) onMessageCreate(session *discordgo.Session, event *discordgo.MessageCreate) {
	if event == nil || event.Message == nil || event.Author == nil || event.Author.Bot || event.GuildID == "" {
		return
	}
	if h.voiceChannelID == "" || event.ChannelID != h.voiceChannelID {
		return
	}
	fields := strings.Fields(event.Content)
	if len(fields) == 0 || fields[0] != "!사다리" {
		return
	}
	reply := func(content string) {
		_, err := session.ChannelMessageSendComplex(event.ChannelID, &discordgo.MessageSend{
			Content: content, Reference: event.Reference(),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		})
		if err != nil {
			log.Printf("draw reply: %v", err)
		}
	}
	n, extra, excluded, err := parse(event.Message)
	if err != nil {
		reply(err.Error())
		return
	}
	if h.voiceChannelID == "" || session.State == nil {
		reply("어드민 음성 채널 정보를 사용할 수 없어요.")
		return
	}
	ids, err := voiceIDs(session.State, event.GuildID, h.voiceChannelID)
	if err != nil {
		reply(err.Error())
		return
	}
	var voice []*discordgo.User
	for _, id := range ids {
		if excluded[id] {
			continue
		}
		member, err := session.State.Member(event.GuildID, id)
		if err != nil {
			member, err = session.GuildMember(event.GuildID, id)
		}
		if err != nil || member == nil || member.User == nil {
			reply("참가자 정보를 확인하지 못했어요. 잠시 후 다시 시도해 주세요.")
			return
		}
		voice = append(voice, member.User)
	}
	pool := candidates(voice, extra, excluded)
	if len(pool) == 0 {
		reply("뽑을 후보가 없어요. 음성 채널에 참가하거나 `!사다리 @사람1 @사람2 1`처럼 후보를 추가해 주세요.")
		return
	}
	ranked, err := h.pick(pool, n, func(id string, result scratch.Result) {
		reply("<@" + id + ">\n" + result.BlockMessage())
	})
	if err != nil {
		reply(err.Error())
		return
	}
	for _, content := range resultMessages(ranked, n, excluded) {
		reply(content)
	}
}
