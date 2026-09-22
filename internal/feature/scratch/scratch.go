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
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const command = "!긁기"

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
	pool   *poolClient
	logger *log.Logger
	mine   bool
	run    func(uint64) (Result, error)
	random func() (int, error)
	send   func(*discordgo.Session, string, string, *discordgo.MessageReference) (*discordgo.Message, error)
}

func New() *Handler {
	pool := newPoolClient()
	return &Handler{
		pool:   pool,
		logger: log.Default(),
		mine:   scratchMiningEnabled,
		run:    pool.run,
		random: randomScore,
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

func (h *Handler) onMessageCreate(session *discordgo.Session, message *discordgo.MessageCreate) {
	if message == nil || message.Message == nil || message.Author == nil || message.Author.Bot {
		return
	}
	if message.GuildID != "" {
		return
	}
	if strings.TrimSpace(message.Content) != command {
		return
	}
	// started := time.Now()
	if !h.mine {
		score, err := h.random()
		if err != nil {
			h.logger.Printf("scratch random user_id=%s: %v", message.Author.ID, err)
			h.reply(session, message, "점수를 뽑다가 문제가 생겼어요.")
			return
		}
		// h.logger.Printf("%dms", time.Since(started).Milliseconds())
		h.reply(session, message, fmt.Sprintf("%d점", score))
		return
	}

	result, err := h.run(defaultAttempts)
	if err != nil {
		h.logger.Printf("scratch: %v", err)
		h.reply(session, message, "복권을 긁다가 문제가 생겼어요.")
		return
	}

	// if result.Submission != "" {
	// 	h.logger.Printf("total=%dms connect=%dms hash=%dms submit=%dms", time.Since(started).Milliseconds(), result.ConnectElapsed.Milliseconds(), result.Elapsed.Milliseconds(), result.SubmitElapsed.Milliseconds())
	// } else {
	// 	h.logger.Printf("total=%dms connect=%dms hash=%dms", time.Since(started).Milliseconds(), result.ConnectElapsed.Milliseconds(), result.Elapsed.Milliseconds())
	// }
	h.reply(session, message, fmt.Sprintf("%d점 - `%s`", result.Score, HashString(result.BestHash)[:7]))
}

func (h *Handler) reply(session *discordgo.Session, message *discordgo.MessageCreate, content string) {
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
