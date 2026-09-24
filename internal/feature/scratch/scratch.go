package scratch

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const command = "!긁기"
const adminChannelFile = "/var/run/secrets/discord-bot/admin_2026_channel_id"
const adminVoiceChannelFile = "/var/run/secrets/discord-bot/admin_voice_channel_id"

var (
	scratchMiningEnabled = true
	defaultAttempts      = uint64(10_000)
)

type Result struct {
	Attempts       uint64
	Score          int
	BestHash       [32]byte
	Elapsed        time.Duration
	ConnectElapsed time.Duration
	SubmitElapsed  time.Duration
	Submission     string
}

type Handler struct {
	adminChannelID      string
	adminVoiceChannelID string
	pool                *poolClient
	logger              *log.Logger
	mine                bool
	run                 func(uint64) (Result, error)
	random              func() (int, error)
	send                func(*discordgo.Session, string, string, *discordgo.MessageReference) (*discordgo.Message, error)
}

func New() *Handler {
	pool := newPoolClient()
	channel, err := os.ReadFile(adminChannelFile)
	if err != nil {
		log.Printf("scratch: admin channel unavailable: %v", err)
	}
	voiceChannel, err := os.ReadFile(adminVoiceChannelFile)
	if err != nil {
		log.Printf("scratch: admin voice channel unavailable: %v", err)
	}
	return &Handler{
		adminChannelID:      strings.TrimSpace(string(channel)),
		adminVoiceChannelID: strings.TrimSpace(string(voiceChannel)),
		pool:                pool,
		logger:              log.Default(),
		mine:                scratchMiningEnabled,
		run:                 pool.run,
		random:              randomScore,
		send: func(session *discordgo.Session, channelID, content string, reference *discordgo.MessageReference) (*discordgo.Message, error) {
			return session.ChannelMessageSendReply(channelID, content, reference)
		},
	}
}

func (h *Handler) Run(ctx context.Context) {
	if h.mine {
		h.pool.serve(ctx, h.logger)
	}
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onMessageCreate)
}

// Mine uses the shared pool and the same work budget as !긁기.
func (h *Handler) Mine() (Result, error) {
	if !h.mine {
		return Result{}, fmt.Errorf("scratch mining is disabled")
	}
	return h.run(defaultAttempts)
}

// BlockMessage preserves the submission status when another feature finds a block.
func (r Result) BlockMessage() string {
	if r.Submission == "" {
		return ""
	}
	return blockMessage(r)
}

func (h *Handler) onMessageCreate(session *discordgo.Session, message *discordgo.MessageCreate) {
	if message == nil || message.Message == nil || message.Author == nil || message.Author.Bot {
		return
	}
	adminAllowed := h.adminChannelID != "" && message.ChannelID == h.adminChannelID
	voiceAllowed := h.adminVoiceChannelID != "" && message.ChannelID == h.adminVoiceChannelID
	if message.GuildID != "" && !adminAllowed && !voiceAllowed {
		return
	}
	target := scratchTarget(message.Message)
	if target == nil {
		return
	}
	// started := time.Now()
	if !h.mine {
		score, err := h.random()
		if err != nil {
			h.logger.Printf("scratch random user_id=%s: %v", message.Author.ID, err)
			h.reply(session, message, target, "점수를 뽑다가 문제가 생겼어요.")
			return
		}
		// h.logger.Printf("%dms", time.Since(started).Milliseconds())
		h.reply(session, message, target, fmt.Sprintf("%d점", score))
		return
	}

	result, err := h.Mine()
	if result.Submission != "" {
		if err != nil {
			h.logger.Printf("scratch submission: %v", err)
		}
		h.reply(session, message, target, blockMessage(result))
		return
	}
	if err != nil {
		h.logger.Printf("scratch: %v", err)
		h.reply(session, message, target, "복권을 긁다가 문제가 생겼어요.")
		return
	}

	// if result.Submission != "" {
	// 	h.logger.Printf("total=%dms connect=%dms hash=%dms submit=%dms", time.Since(started).Milliseconds(), result.ConnectElapsed.Milliseconds(), result.Elapsed.Milliseconds(), result.SubmitElapsed.Milliseconds())
	// } else {
	// 	h.logger.Printf("total=%dms connect=%dms hash=%dms", time.Since(started).Milliseconds(), result.ConnectElapsed.Milliseconds(), result.Elapsed.Milliseconds())
	// }
	h.reply(session, message, target, fmt.Sprintf("%d점 - `%s`", result.Score, HashString(result.BestHash)[:7]))
}

func blockMessage(result Result) string {
	hash := HashString(result.BestHash)
	heading := "블록 조건을 충족하는 해시를 발견했습니다."
	status := "⚠️ 제출 승인 여부를 확인하지 못했습니다. 아래 링크에서 블록 반영 여부를 확인해 주세요."
	switch result.Submission {
	case "accepted":
		heading = "🎆🎉🎊 **블록을 발견했습니다!!!** 🎊🎉🎆"
		status = "✅ 제출이 승인되었습니다!\n아래 링크에서 블록 반영 여부를 확인해 주세요."
	case "rejected":
		status = "⚠️ 제출이 거절되었습니다. 보상은 확인되지 않았습니다."
	}
	return fmt.Sprintf("%s\n\n블록 해시:\n`%s`\n\n%s\n\n[블록 확인하기](https://mempool.space/block/%s)", heading, hash, status, hash)
}

func scratchTarget(message *discordgo.Message) *discordgo.User {
	fields := strings.Fields(message.Content)
	if len(fields) == 0 || fields[0] != command {
		return nil
	}
	if len(fields) == 1 {
		return message.Author
	}
	if len(fields) != 2 {
		return nil
	}
	for _, user := range message.Mentions {
		if user != nil && (fields[1] == "<@"+user.ID+">" || fields[1] == "<@!"+user.ID+">") {
			return user
		}
	}
	return nil
}

func (h *Handler) reply(session *discordgo.Session, message *discordgo.MessageCreate, target *discordgo.User, content string) {
	content = target.Mention() + "\n" + content
	if _, err := h.send(session, message.ChannelID, content, message.Reference()); err != nil {
		h.logger.Printf("send scratch reply channel_id=%s message_id=%s: %v", message.ChannelID, message.ID, err)
	}
}

func randomScore() (int, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(100))
	if err != nil {
		return 0, fmt.Errorf("generate random score: %w", err)
	}
	return int(value.Int64()) + 1, nil
}

func scoreBestHash(best [32]byte, attempts uint64) int {
	mostSignificant := binary.LittleEndian.Uint64(best[24:32])
	normalized := math.Ldexp(float64(mostSignificant), -64)
	luckPercentile := math.Exp(float64(attempts) * math.Log1p(-normalized))
	score := 1 + int(math.Floor(99*luckPercentile))
	if score < 1 {
		return 1
	}
	if score > 99 {
		return 99
	}
	return score
}

func lessBitcoinHash(left, right [32]byte) bool {
	for i := len(left) - 1; i >= 0; i-- {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return false
}

func HashString(hash [32]byte) string {
	var displayed [32]byte
	for i := range hash {
		displayed[i] = hash[len(hash)-1-i]
	}
	return hex.EncodeToString(displayed[:])
}
