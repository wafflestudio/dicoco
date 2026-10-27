package agent

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/wafflestudio/dicoco/internal/opencode"
)

type modelEngine interface {
	Models(context.Context) ([]opencode.Model, error)
	SelectModel(context.Context, string) (opencode.Model, error)
	CurrentModel(context.Context) (string, error)
}

const modelUsage = "DM에서 사용해 주세요.\n`모델 목록` — 사용 가능한 무료 모델\n`모델 현재` — 봇 전체 모델 확인\n`모델 변경 big-pickle` — 봇 전체 모델 변경"

func modelCommand(text string) (string, string) {
	text = strings.TrimSpace(text)
	switch text {
	case "모델", "모델 현재", "/model":
		return "current", ""
	case "모델 목록", "/models":
		return "list", ""
	case "모델 변경":
		return "usage", ""
	}
	for _, prefix := range []string{"모델 변경 ", "/model "} {
		if strings.HasPrefix(text, prefix) {
			selector := strings.TrimSpace(strings.TrimPrefix(text, prefix))
			if selector == "" {
				return "usage", ""
			}
			return "select", selector
		}
	}
	return "", ""
}

// Commands are handled directly by Go, so changing a model still works when
// the current model is rate limited. They do not create an agent conversation.
func (h *Handler) handleModelCommand(s discordAPI, botID string, m *discordgo.MessageCreate) bool {
	text := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(m.Content, "<@"+botID+">", ""), "<@!"+botID+">", ""))
	command, selector := modelCommand(text)
	if command == "" {
		return false
	}
	if m.GuildID != "" {
		// Untagged server chatter is not an agent request.
		if strings.Contains(m.Content, "<@"+botID+">") || strings.Contains(m.Content, "<@!"+botID+">") {
			h.send(s, m.ChannelID, "모델 설정은 봇 DM에서만 할 수 있어요.\n"+modelUsage)
		}
		return true
	}
	if command == "usage" {
		h.send(s, m.ChannelID, modelUsage)
		return true
	}
	engine, ok := h.engine.(modelEngine)
	if !ok {
		h.send(s, m.ChannelID, "현재 엔진은 모델 변경을 지원하지 않아요.")
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var reply string
	var err error
	switch command {
	case "current":
		var current string
		current, err = engine.CurrentModel(ctx)
		reply = "봇 전체 모델: " + current
	case "list":
		var models []opencode.Model
		models, err = engine.Models(ctx)
		var lines []string
		for _, model := range models {
			lines = append(lines, "• `"+model.String()+"` — "+model.Name)
		}
		if len(lines) == 0 {
			reply = "현재 선택 가능한 무료 모델이 없어요."
		} else {
			reply = "사용 가능한 무료 모델:\n" + strings.Join(lines, "\n") + "\n\n`모델 변경 모델ID`로 봇 전체에 적용할 수 있어요."
		}
	case "select":
		var model opencode.Model
		model, err = engine.SelectModel(ctx, selector)
		if err == nil {
			log.Printf("[agent] global model changed by user %s: %s", m.Author.ID, model.String())
			reply = "봇 전체 모델을 `" + model.String() + "`로 변경했어요. 진행 중인 작업은 유지하고, 다음 처리 요청부터 모든 대화에 적용해요."
		}
	}
	if errors.Is(err, opencode.ErrModelUnavailable) {
		reply = "선택 가능한 무료 모델을 찾지 못했어요. `모델 목록`에 나온 전체 ID로 지정해 주세요."
	} else if err != nil {
		log.Printf("[agent] model command %s failed: %v", command, err)
		reply = "모델 설정 중 오류가 발생했어요. 봇 로그를 확인해 주세요."
	}
	h.send(s, m.ChannelID, reply)
	return true
}
