package opsreminder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

type notionRichText struct {
	PlainText string `json:"plain_text"`
	Href      string `json:"href"`
	Mention   *struct {
		Type string `json:"type"`
		User *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"user"`
	} `json:"mention"`
}
type notionPage struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	LastEdited string `json:"last_edited_time"`
	Properties map[string]struct {
		Date *struct {
			Start string `json:"start"`
		} `json:"date"`
	} `json:"properties"`
}
type notionBlock struct {
	ID          string                     `json:"id"`
	ParentID    string                     `json:"-"`
	Type        string                     `json:"type"`
	HasChildren bool                       `json:"has_children"`
	LastEdited  string                     `json:"last_edited_time"`
	Data        map[string]json.RawMessage `json:"-"`
}

func (b *notionBlock) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_ = json.Unmarshal(raw["id"], &b.ID)
	_ = json.Unmarshal(raw["type"], &b.Type)
	_ = json.Unmarshal(raw["has_children"], &b.HasChildren)
	_ = json.Unmarshal(raw["last_edited_time"], &b.LastEdited)
	b.Data = raw
	return nil
}

type notionCollector struct {
	api         *apiClient
	token       string
	meetingID   string
	meetingDate string
	now         time.Time
}

func (n notionCollector) query(ctx context.Context, id string, filter any) ([]notionPage, error) {
	body := map[string]any{"page_size": 100, "filter": filter}
	var pages []notionPage
	for page := 0; page < 30; page++ {
		var response struct {
			Results    []notionPage `json:"results"`
			HasMore    bool         `json:"has_more"`
			NextCursor string       `json:"next_cursor"`
		}
		err := n.api.request(ctx, "POST", "https://api.notion.com/v1/data_sources/"+url.PathEscape(id)+"/query", notionHeaders(n.token), body, &response, true)
		if err != nil {
			return nil, err
		}
		pages = append(pages, response.Results...)
		if !response.HasMore {
			return pages, nil
		}
		if response.NextCursor == "" {
			return nil, fmt.Errorf("Notion query cursor missing")
		}
		body["start_cursor"] = response.NextCursor
	}
	return nil, fmt.Errorf("Notion query exceeded 30 pages")
}
func (n notionCollector) blocks(ctx context.Context, id string, depth int, out *[]notionBlock) error {
	if depth > 7 {
		return fmt.Errorf("Notion block depth exceeded")
	}
	endpoint := "https://api.notion.com/v1/blocks/" + url.PathEscape(id) + "/children?page_size=100"
	for page := 0; page < 10; page++ {
		var response struct {
			Results    []notionBlock `json:"results"`
			HasMore    bool          `json:"has_more"`
			NextCursor string        `json:"next_cursor"`
		}
		if err := n.api.request(ctx, "GET", endpoint, notionHeaders(n.token), nil, &response, true); err != nil {
			return err
		}
		for _, block := range response.Results {
			block.ParentID = id
			*out = append(*out, block)
			if len(*out) > 500 {
				return fmt.Errorf("Notion page exceeds 500 blocks")
			}
			if block.HasChildren && block.Type != "child_database" && block.Type != "child_page" {
				if err := n.blocks(ctx, block.ID, depth+1, out); err != nil {
					return err
				}
			}
		}
		if !response.HasMore {
			return nil
		}
		if response.NextCursor == "" {
			return fmt.Errorf("Notion block cursor missing")
		}
		endpoint = "https://api.notion.com/v1/blocks/" + url.PathEscape(id) + "/children?page_size=100&start_cursor=" + url.QueryEscape(response.NextCursor)
	}
	return fmt.Errorf("Notion block pagination exceeded 10 pages")
}

var discordThreadURL = regexp.MustCompile(`https?://(?:www\.)?discord(?:app)?\.com/channels/[0-9]+/([0-9]+)(?:/[0-9]+)?`)
var possibleAction = regexp.MustCompile(`(?i)(todo|할\s*일|해야|하기로|담당|마감|까지|준비|제출|공지|연락|확인|정리|작성|신청|예약|진행)`)
var followupAction = regexp.MustCompile(`(?i)(todo|할\s*일|해야|하기로|조사하기|찾기|물어보기|작성|보내기|전송|제출|공지|신청|예약|준비|제작|확인하기|정리하기|선정하기)`)
var followupProposal = regexp.MustCompile(`(좋을듯|좋지 않을까|어떨까|하면 좋|\?)`)
var dateHint = regexp.MustCompile(`(?:20[0-9]{2}[-/])?[0-9]{1,2}[-/][0-9]{1,2}|[0-9]{1,2}월\s*[0-9]{1,2}일`)

func meetingTODOBlock(b notionBlock, text string) bool {
	if text == "" {
		return false
	}
	return b.Type == "to_do" || ((b.Type == "bulleted_list_item" || b.Type == "numbered_list_item" || b.Type == "paragraph") && possibleAction.MatchString(text))
}

func richText(parts []notionRichText) string {
	var s strings.Builder
	for _, p := range parts {
		s.WriteString(p.PlainText)
		if p.Href != "" && !strings.Contains(p.PlainText, p.Href) {
			s.WriteString(" (")
			s.WriteString(p.Href)
			s.WriteString(")")
		}
	}
	return strings.TrimSpace(s.String())
}
func blockContent(b notionBlock) (string, []string, bool) {
	var v struct {
		RichText []notionRichText `json:"rich_text"`
		Checked  bool             `json:"checked"`
		Title    string           `json:"title"`
	}
	_ = json.Unmarshal(b.Data[b.Type], &v)
	keys := []string{}
	for _, p := range v.RichText {
		if p.Mention != nil && p.Mention.User != nil && p.Mention.User.Name != "" {
			keys = append(keys, cleanPersonName(p.Mention.User.Name))
		}
	}
	text := richText(v.RichText)
	if text == "" {
		text = v.Title
	}
	return text, uniqueStrings(keys), v.Checked
}
func notionEvidence(id, url, text, kind, at string) Evidence {
	t, _ := time.Parse(time.RFC3339, at)
	return Evidence{Ref: id, URL: url, Text: text, Kind: kind, At: t}
}
func threadIDs(texts ...string) []string {
	var ids []string
	for _, text := range texts {
		for _, m := range discordThreadURL.FindAllStringSubmatch(text, -1) {
			ids = append(ids, m[1])
		}
	}
	return uniqueStrings(ids)
}
func uniqueStrings(v []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range v {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
func isClosed(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	for _, term := range []string{"완료", "취소", "보류", "안 함", "하지 않음", "done", "canceled", "cancelled"} {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}
func (n notionCollector) collect(ctx context.Context) ([]Candidate, error) {
	today := n.now.Format("2006-01-02")
	past := n.now.AddDate(0, 0, -29).Format("2006-01-02")
	if n.meetingDate != "" {
		past, today = n.meetingDate, n.meetingDate
	}
	candidates := []Candidate{}
	meetings, err := n.query(ctx, n.meetingID, map[string]any{"and": []any{
		map[string]any{"property": "Date", "date": map[string]string{"on_or_after": past}},
		map[string]any{"property": "Date", "date": map[string]string{"on_or_before": today}},
	}})
	if err != nil {
		return nil, fmt.Errorf("Notion meeting query: %w", err)
	}
	for _, p := range meetings {
		var blocks []notionBlock
		if err := n.blocks(ctx, p.ID, 0, &blocks); err != nil {
			return nil, fmt.Errorf("Notion meeting %s: %w", p.ID, err)
		}
		blockByID := make(map[string]notionBlock, len(blocks))
		for _, block := range blocks {
			blockByID[block.ID] = block
		}
		inAgenda := false
		for i, b := range blocks {
			text, keys, checked := blockContent(b)
			if n.meetingDate != "" {
				if b.ParentID == p.ID && b.Type == "heading_2" {
					inAgenda = text == "안건" || text == "결론"
				}
				if !inAgenda {
					continue
				}
				if text == "" || (b.Type != "to_do" && (b.Type != "bulleted_list_item" && b.Type != "numbered_list_item" && b.Type != "paragraph" || !followupAction.MatchString(text) && !(strings.Contains(text, "@") && strings.Contains(text, "까지")))) {
					continue
				}
				if followupProposal.MatchString(text) && !strings.Contains(text, "해야") {
					continue
				}
				if parent, ok := blockByID[b.ParentID]; ok && parent.Type != "heading_2" && parent.Type != "heading_3" {
					parentText, _, _ := blockContent(parent)
					if len(keys) == 0 && (followupAction.MatchString(parentText) || strings.Contains(parentText, "@") && strings.Contains(parentText, "까지")) {
						continue
					}
				}
			} else if !meetingTODOBlock(b, text) {
				continue
			}
			url := p.URL + "#" + strings.ReplaceAll(b.ID, "-", "")
			ev := notionEvidence(b.ID, url, text, "meeting_"+b.Type, b.LastEdited)
			c := Candidate{ID: b.ID, URL: url, Title: text, AssigneeKeys: keys, Checked: checked, ThreadIDs: threadIDs(text), Evidence: []Evidence{ev}}
			// The action text often omits its subject: e.g. "visit before use" is nested under a venue.
			parentID := b.ParentID
			sectionID := ""
			for level := 0; level < 5; level++ {
				parent, ok := blockByID[parentID]
				if !ok {
					break
				}
				parentText, _, _ := blockContent(parent)
				if parent.Type == "heading_2" {
					sectionID = parent.ID
				}
				if parentText != "" {
					parentURL := p.URL + "#" + strings.ReplaceAll(parent.ID, "-", "")
					c.Evidence = append(c.Evidence, notionEvidence(parent.ID, parentURL, parentText, "meeting_parent", parent.LastEdited))
				}
				parentID = parent.ParentID
			}
			// Static heading_3 blocks are siblings of their callouts, so ancestry misses the topic.
			if sectionID != "" {
				for j := i - 1; j >= 0; j-- {
					if blocks[j].ParentID == sectionID && blocks[j].Type == "heading_3" {
						topic, _, _ := blockContent(blocks[j])
						if topic != "" {
							topicURL := p.URL + "#" + strings.ReplaceAll(blocks[j].ID, "-", "")
							c.Evidence = append(c.Evidence, notionEvidence(blocks[j].ID, topicURL, topic, "meeting_topic", blocks[j].LastEdited))
						}
						break
					}
				}
			}
			if b.HasChildren {
				added := 0
				for _, detail := range blocks {
					if detail.ParentID != b.ID {
						continue
					}
					detailText, _, _ := blockContent(detail)
					if detailText == "" {
						continue
					}
					detailURL := p.URL + "#" + strings.ReplaceAll(detail.ID, "-", "")
					c.Evidence = append(c.Evidence, notionEvidence(detail.ID, detailURL, detailText, "meeting_detail", detail.LastEdited))
					added++
					if added == 6 {
						break
					}
				}
			}
			// Nearby meeting text can contain the deadline or explicit owner for a TODO block.
			for j := max(0, i-2); j <= min(len(blocks)-1, i+2); j++ {
				if j == i {
					continue
				}
				if blocks[j].ParentID != b.ParentID {
					continue
				}
				contextText, _, _ := blockContent(blocks[j])
				if contextText == "" || meetingTODOBlock(blocks[j], contextText) {
					continue
				}
				contextURL := p.URL + "#" + strings.ReplaceAll(blocks[j].ID, "-", "")
				c.Evidence = append(c.Evidence, notionEvidence(blocks[j].ID, contextURL, contextText, "meeting_context", blocks[j].LastEdited))
				c.ThreadIDs = append(c.ThreadIDs, threadIDs(contextText)...)
			}
			c.ThreadIDs = uniqueStrings(c.ThreadIDs)
			candidates = append(candidates, c)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	return candidates, nil
}
func cleanPersonName(name string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.Split(name, " / ")[0], "@"))
}
