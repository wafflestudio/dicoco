package githubpr

import (
	"fmt"
	"log"
	"regexp"

	"github.com/bwmarrin/discordgo"
)

var wafflestudioPRRegex = regexp.MustCompile(`github\.com/wafflestudio/([A-Za-z0-9_.-]+)/pull/([0-9]+)`)

type Handler struct{}

func New() *Handler {
	return &Handler{}
}

func (h *Handler) Register(session *discordgo.Session) {
	session.AddHandler(h.onMessageCreate)
}

func (h *Handler) onMessageCreate(session *discordgo.Session, message *discordgo.MessageCreate) {
	if message == nil || message.Message == nil || message.Author == nil || message.Author.Bot {
		return
	}

	for _, url := range extractPRURLs(message.Content) {
		log.Printf("[github_pr] detected PR URL: %s", url)
	}
}

func extractPRURLs(content string) []string {
	matches := wafflestudioPRRegex.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	urls := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		canonicalURL := fmt.Sprintf("github.com/wafflestudio/%s/pull/%s", match[1], match[2])
		if _, ok := seen[canonicalURL]; ok {
			continue
		}
		seen[canonicalURL] = struct{}{}
		urls = append(urls, canonicalURL)
	}
	return urls
}
