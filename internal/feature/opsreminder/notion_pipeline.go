package opsreminder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

type scheduledMeeting struct {
	Page notionPage
	Date string
}
type managedTask struct {
	BlockID, Owner, Title, DueDate, SourceURL, URL string
	DefaultDeadline                                bool
	Checked                                        bool
	ThreadIDs                                      []string
	LastEdited                                     string
	ThreadURL                                      string
}

const defaultDeadlineText = "다음 회의 전 (회의록에 데드라인을 명시해주세요)"

func meetingDate(p notionPage) string {
	if d, ok := p.Properties["Date"]; ok && d.Date != nil {
		return trimDate(d.Date.Start)
	}
	return ""
}

// The Date property is the schedule. created_time is unsuitable because future templates already exist.
func (n notionCollector) meetingPair(ctx context.Context) (*scheduledMeeting, *scheduledMeeting, error) {
	today := n.now.Format("2006-01-02")
	pages, err := n.query(ctx, n.meetingID, map[string]any{"and": []any{
		map[string]any{"property": "Date", "date": map[string]string{"on_or_after": n.now.AddDate(0, 0, -45).Format("2006-01-02")}},
		map[string]any{"property": "Date", "date": map[string]string{"on_or_before": n.now.AddDate(0, 0, 90).Format("2006-01-02")}},
	}})
	if err != nil {
		return nil, nil, err
	}
	var previous, upcoming *scheduledMeeting
	counts := map[string]int{}
	for _, p := range pages {
		d := meetingDate(p)
		if d == "" {
			continue
		}
		counts[d]++
		m := &scheduledMeeting{Page: p, Date: d}
		if d < today && (previous == nil || d > previous.Date) {
			previous = m
		}
		if d >= today && (upcoming == nil || d < upcoming.Date) {
			upcoming = m
		}
	}
	if previous != nil && counts[previous.Date] > 1 {
		return nil, nil, fmt.Errorf("multiple meeting pages share Date %s", previous.Date)
	}
	if upcoming != nil && counts[upcoming.Date] > 1 {
		return nil, nil, fmt.Errorf("multiple meeting pages share Date %s", upcoming.Date)
	}
	return previous, upcoming, nil
}

func (n notionCollector) directChildren(ctx context.Context, id string) ([]notionBlock, error) {
	var out []notionBlock
	endpoint := "https://api.notion.com/v1/blocks/" + url.PathEscape(id) + "/children?page_size=100"
	for page := 0; page < 10; page++ {
		var x struct {
			Results    []notionBlock `json:"results"`
			HasMore    bool          `json:"has_more"`
			NextCursor string        `json:"next_cursor"`
		}
		if err := n.api.request(ctx, "GET", endpoint, notionHeaders(n.token), nil, &x, true); err != nil {
			return nil, err
		}
		for _, b := range x.Results {
			b.ParentID = id
			out = append(out, b)
		}
		if len(out) > 500 {
			return nil, fmt.Errorf("Notion block exceeds 500 children")
		}
		if !x.HasMore {
			return out, nil
		}
		if x.NextCursor == "" {
			return nil, fmt.Errorf("Notion block cursor missing")
		}
		endpoint = "https://api.notion.com/v1/blocks/" + url.PathEscape(id) + "/children?page_size=100&start_cursor=" + url.QueryEscape(x.NextCursor)
	}
	return nil, fmt.Errorf("Notion block pagination exceeded")
}

func (n notionCollector) appendChildren(ctx context.Context, parent string, children []any, after string) ([]notionBlock, error) {
	body := map[string]any{"children": children}
	if after != "" {
		body["position"] = map[string]any{"type": "after_block", "after_block": map[string]string{"id": after}}
	}
	var x struct {
		Results []notionBlock `json:"results"`
	}
	err := n.api.request(ctx, "PATCH", "https://api.notion.com/v1/blocks/"+url.PathEscape(parent)+"/children", notionHeaders(n.token), body, &x, false)
	return x.Results, err
}
func (n notionCollector) setChecked(ctx context.Context, id string) error {
	body := map[string]any{"to_do": map[string]any{"checked": true}}
	return n.api.request(ctx, "PATCH", "https://api.notion.com/v1/blocks/"+url.PathEscape(id), notionHeaders(n.token), body, nil, false)
}
func notionText(text string) map[string]any {
	return map[string]any{"type": "text", "text": map[string]any{"content": text}}
}
func notionLink(label, href string) map[string]any {
	return map[string]any{"type": "text", "text": map[string]any{"content": label, "link": map[string]string{"url": href}}}
}
func todoBlock(t managedTask) map[string]any {
	due := t.DueDate
	if t.DefaultDeadline {
		due = defaultDeadlineText
	}
	parts := []any{notionText(t.Title + " · 기한: " + due + " · "), notionLink("출처", t.SourceURL)}
	if t.ThreadURL != "" {
		parts = append(parts, notionText(" · "), notionLink("스레드", t.ThreadURL))
	}
	return map[string]any{"object": "block", "type": "to_do", "to_do": map[string]any{"rich_text": parts, "checked": false}}
}
func ownerBlock(name string, child map[string]any) map[string]any {
	return map[string]any{"object": "block", "type": "bulleted_list_item", "bulleted_list_item": map[string]any{"rich_text": []any{notionText(name)}, "children": []any{child}}}
}
func (n notionCollector) todoHeading(ctx context.Context, p notionPage, create bool) (string, error) {
	roots, err := n.directChildren(ctx, p.ID)
	if err != nil {
		return "", err
	}
	var conclusion, heading string
	for _, b := range roots {
		t, _, _ := blockContent(b)
		if b.Type == "heading_2" && t == "결론" {
			conclusion = b.ID
		}
		if b.Type == "heading_2" && t == "TODO" {
			heading = b.ID
		}
	}
	if heading != "" {
		return heading, nil
	}
	if !create {
		return "", nil
	}
	if conclusion == "" {
		return "", fmt.Errorf("meeting %s has no conclusion heading", p.ID)
	}
	child := map[string]any{"object": "block", "type": "heading_2", "heading_2": map[string]any{"rich_text": []any{notionText("TODO")}, "is_toggleable": true}}
	added, err := n.appendChildren(ctx, p.ID, []any{child}, conclusion)
	if err != nil {
		return "", err
	}
	if len(added) != 1 || added[0].ID == "" {
		return "", fmt.Errorf("Notion TODO heading creation unconfirmed")
	}
	return added[0].ID, nil
}
func parseManagedTask(b notionBlock, owner, pageURL, meetingDate string) (managedTask, error) {
	var raw struct {
		RichText []notionRichText `json:"rich_text"`
		Checked  bool             `json:"checked"`
	}
	if err := json.Unmarshal(b.Data["to_do"], &raw); err != nil {
		return managedTask{}, err
	}
	title := ""
	if len(raw.RichText) > 0 {
		title = raw.RichText[0].PlainText
	}
	title, due, _ := strings.Cut(title, " · 기한: ")
	due = strings.TrimSpace(strings.TrimSuffix(due, " · "))
	defaultDeadline := due == defaultDeadlineText || due == "미정"
	if defaultDeadline {
		due = meetingDate
	}
	source, thread := "", ""
	for _, part := range raw.RichText {
		if part.Href != "" && part.PlainText == "출처" {
			source = part.Href
		}
		if part.Href != "" && part.PlainText == "스레드" {
			thread = part.Href
		}
	}
	if title == "" || source == "" {
		return managedTask{}, fmt.Errorf("managed TODO %s lacks title or source", b.ID)
	}
	return managedTask{BlockID: b.ID, Owner: owner, Title: title, DueDate: due, DefaultDeadline: defaultDeadline, SourceURL: source, URL: pageURL + "#" + strings.ReplaceAll(b.ID, "-", ""), Checked: raw.Checked, ThreadIDs: uniqueStrings(append(threadIDs(title), threadIDs(thread)...)), LastEdited: b.LastEdited, ThreadURL: thread}, nil
}
func (n notionCollector) managedTasks(ctx context.Context, heading, pageURL, meetingDate string) ([]managedTask, map[string]string, error) {
	if heading == "" {
		return nil, map[string]string{}, nil
	}
	groups, err := n.directChildren(ctx, heading)
	if err != nil {
		return nil, nil, err
	}
	owners := map[string]string{}
	tasks := []managedTask{}
	for _, group := range groups {
		if group.Type != "bulleted_list_item" {
			continue
		}
		owner, _, _ := blockContent(group)
		owner = strings.TrimSpace(owner)
		if owner == "" {
			continue
		}
		if owners[owner] != "" {
			return nil, nil, fmt.Errorf("duplicate TODO owner group %s", owner)
		}
		owners[owner] = group.ID
		if !group.HasChildren {
			continue
		}
		children, err := n.directChildren(ctx, group.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, b := range children {
			if b.Type != "to_do" {
				continue
			}
			t, err := parseManagedTask(b, owner, pageURL, meetingDate)
			if err != nil {
				return nil, nil, err
			}
			tasks = append(tasks, t)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].BlockID < tasks[j].BlockID })
	return tasks, owners, nil
}
func (n notionCollector) sourceBlock(ctx context.Context, sourceURL string) (notionBlock, error) {
	u, err := url.Parse(sourceURL)
	if err != nil {
		return notionBlock{}, err
	}
	id := u.Fragment
	if len(id) != 32 {
		return notionBlock{}, fmt.Errorf("source URL has no block id")
	}
	var b notionBlock
	err = n.api.request(ctx, "GET", "https://api.notion.com/v1/blocks/"+url.PathEscape(id), notionHeaders(n.token), nil, &b, true)
	return b, err
}
func sourceEditedOnMeetingDay(m scheduledMeeting) bool {
	edited, err := time.Parse(time.RFC3339, m.Page.LastEdited)
	if err != nil {
		return false
	}
	loc, _ := time.LoadLocation("Asia/Seoul")
	return edited.In(loc).Format("2006-01-02") >= m.Date
}
