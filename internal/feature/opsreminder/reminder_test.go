package opsreminder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func TestDecisionRequiresCitedDueDateAndMappedOwner(t *testing.T) {
	c := Candidate{ID: "task-1", URL: "https://notion.so/task-1", Title: "공지 작성", ThreadIDs: []string{"222222222222222222"}, AssigneeKeys: []string{"person-1"}, Evidence: []Evidence{{Ref: "task-1", Text: "due=2026-11-09; assignees=person-1"}}}
	due := "2026-11-09"
	d := Decision{CandidateID: c.ID, Status: "open", DueDate: &due, AssigneeKeys: []string{"person-1"}, EvidenceRefs: []string{"task-1"}}
	mapped := map[string]string{"person-1": "123456789012345678"}
	r, reason := validDecision(c, d, mapped, due)
	if reason != "" || r.DueDate != due || len(r.UserIDs) != 1 {
		t.Fatalf("valid decision rejected: %s %#v", reason, r)
	}
	d.EvidenceRefs = []string{"unknown"}
	if _, reason := validDecision(c, d, mapped, due); reason == "" {
		t.Fatal("unknown evidence accepted")
	}
	d.EvidenceRefs = []string{"task-1"}
	other := "2026-11-10"
	d.DueDate = &other
	if _, reason := validDecision(c, d, mapped, other); reason != "" {
		t.Fatalf("model-verified changed deadline rejected by text heuristic: %s", reason)
	}
	c.Checked = true
	d.DueDate = &due
	if _, reason := validDecision(c, d, mapped, due); reason != "" {
		t.Fatalf("checked task was hard-blocked despite verified open status: %s", reason)
	}
}
func TestDiscordDuplicateUsesNotionURLAndDueDate(t *testing.T) {
	d := discordClient{botID: "123456789012345678"}
	r := Reminder{NotionURL: "https://notion.so/one", DueDate: "2026-11-09"}
	content := "⏰ 운영팀 TODO · 내일 마감\n\n<@1> 작업\n마감: 2026-11-09 (KST)\n근거: https://notion.so/one"
	messages := []discordMessage{{Content: content}}
	messages[0].Author.ID = d.botID
	if !d.alreadySent(messages, r) {
		t.Fatal("existing reminder not detected")
	}
	r.DueDate = "2026-11-10"
	if d.alreadySent(messages, r) {
		t.Fatal("changed due date treated as duplicate")
	}
	r.DueDate = "2026-11-09"
	messages[0].Author.ID = "999999999999999999"
	if d.alreadySent(messages, r) {
		t.Fatal("another author's message treated as duplicate")
	}
}
func TestOpenRouterUsesStructuredResponseWithoutStoring(t *testing.T) {
	api := newAPIClient()
	api.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "openrouter.ai" || req.URL.Path != "/api/v1/responses" || req.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("wrong endpoint or auth: %s", req.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["store"] != false {
			t.Fatal("response storage was not disabled")
		}
		format := body["text"].(map[string]any)["format"].(map[string]any)
		if format["type"] != "json_schema" || format["strict"] != true {
			t.Fatal("structured output missing")
		}
		return response(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{\"candidate_id\":\"task-1\",\"status\":\"done\",\"due_date\":null,\"assignee_keys\":[],\"reason\":\"Done\",\"evidence_refs\":[\"e1\"]}"}]}]}`), nil
	})
	c := Candidate{ID: "task-1", Evidence: []Evidence{{Ref: "e1", At: time.Now()}}}
	d, err := (openAIClient{api: api, key: "test-key", model: "test-model"}).decide(context.Background(), c, nil)
	if err != nil || d.Status != "done" {
		t.Fatalf("unexpected decision %#v %v", d, err)
	}
}

func TestDirectOpenAIUsesResponsesEndpoint(t *testing.T) {
	api := newAPIClient()
	api.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.openai.com" || req.URL.Path != "/v1/responses" || req.Header.Get("Authorization") != "Bearer test-openai-key" {
			t.Fatalf("wrong direct OpenAI request: %s", req.URL)
		}
		return response(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{\"candidate_id\":\"task-1\",\"status\":\"done\",\"due_date\":null,\"assignee_keys\":[],\"reason\":\"완료\",\"evidence_refs\":[\"e1\"]}"}]}]}`), nil
	})
	c := Candidate{ID: "task-1", Evidence: []Evidence{{Ref: "e1", At: time.Now()}}}
	d, err := (openAIClient{api: api, key: "test-openai-key", model: "gpt-5-mini", endpoint: "https://api.openai.com/v1/responses"}).decide(context.Background(), c, nil)
	if err != nil || d.Status != "done" {
		t.Fatalf("direct OpenAI decision failed: %#v %v", d, err)
	}
}
func TestDiscordSendLimitsMentionsAndDoesNotRetryPost(t *testing.T) {
	posts := 0
	api := newAPIClient()
	api.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		posts++
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		mentions := body["allowed_mentions"].(map[string]any)
		if _, ok := mentions["parse"]; ok {
			t.Fatal("parse mentions should be absent")
		}
		users := mentions["users"].([]any)
		if len(users) != 1 || users[0] != "123456789012345678" {
			t.Fatal("wrong mention allowlist")
		}
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("error")), Header: make(http.Header)}, nil
	})
	d := discordClient{api: api, token: "token", reminderID: "111111111111111111"}
	_, err := d.send(context.Background(), Reminder{Title: "작업", DueDate: "2026-11-09", NotionURL: "https://notion.so/one", UserIDs: []string{"123456789012345678"}})
	if err == nil || posts != 1 {
		t.Fatalf("post retried or error ignored: posts=%d err=%v", posts, err)
	}
}

func TestConversationCompletionIsHandledByModelStatus(t *testing.T) {
	due := "2026-11-09"
	c := Candidate{ID: "task-1", URL: "https://notion.so/task-1", Title: "공지", ThreadIDs: []string{"222222222222222222"}, AssigneeKeys: []string{"person-1"}, Evidence: []Evidence{
		{Ref: "task-1", Text: "due=2026-11-09; assignees=person-1"},
		{Ref: "done-message", Kind: "discord_thread_message", Text: "이 작업 Done", At: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)},
	}}
	d := Decision{CandidateID: c.ID, Status: "open", DueDate: &due, AssigneeKeys: []string{"person-1"}, EvidenceRefs: []string{"task-1"}}
	mapping := map[string]string{"person-1": "123456789012345678"}
	if _, reason := validDecision(c, d, mapping, due); reason != "" {
		t.Fatalf("text heuristic overrode model decision: %s", reason)
	}
	d.Status = "done"
	if _, reason := validDecision(c, d, mapping, due); reason == "" {
		t.Fatal("model-completed task was accepted")
	}
}

func TestRunSendsOnceAndFindsExistingDiscordMessage(t *testing.T) {
	now := time.Date(2026, 11, 8, 9, 0, 0, 0, time.FixedZone("KST", 9*3600))
	posts := 0
	posted := ""
	modelCalls := 0
	api := newAPIClient()
	api.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		path := req.URL.Path
		switch {
		case path == "/api/v10/users/@me":
			return response(`{"id":"111111111111111111"}`), nil
		case path == "/api/v10/channels/222222222222222222":
			return response(`{"id":"222222222222222222","guild_id":"444444444444444444"}`), nil
		case path == "/api/v10/channels/333333333333333333":
			return response(`{"id":"333333333333333333","guild_id":"444444444444444444"}`), nil
		case path == "/api/v10/channels/555555555555555555":
			return response(`{"id":"555555555555555555","guild_id":"444444444444444444"}`), nil
		case path == "/v1/data_sources/meetings/query":
			return response(`{"results":[{"id":"meeting-1","url":"https://notion.so/meeting-1","last_edited_time":"2026-11-08T00:00:00Z"}],"has_more":false}`), nil
		case path == "/v1/blocks/meeting-1/children":
			return response(`{"results":[{"id":"task-1","type":"to_do","last_edited_time":"2026-11-08T00:00:00Z","to_do":{"rich_text":[{"plain_text":"담당자 공지 작성, 11/9까지. 스레드: https://discord.com/channels/444444444444444444/555555555555555555"}],"checked":false}}],"has_more":false}`), nil
		case path == "/api/v10/channels/222222222222222222/messages":
			return response(`[]`), nil
		case path == "/api/v10/channels/555555555555555555/messages":
			return response(`[]`), nil
		case path == "/api/v10/channels/333333333333333333/messages" && req.Method == "GET":
			if posted == "" {
				return response(`[]`), nil
			}
			b, _ := json.Marshal([]map[string]any{{"id": "666666666666666666", "channel_id": "333333333333333333", "content": posted, "timestamp": "2026-11-08T01:00:00Z", "author": map[string]any{"id": "111111111111111111", "bot": true}}})
			return response(string(b)), nil
		case path == "/api/v1/responses":
			modelCalls++
			if modelCalls%2 == 1 {
				return response(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{\"candidate_id\":\"task-1\",\"status\":\"open\",\"due_date\":\"2026-11-09\",\"assignee_keys\":[\"담당자\"],\"reason\":\"회의록에 배정됨\",\"evidence_refs\":[\"task-1\"]}"}]}]}`), nil
			}
			return response(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{\"candidate_id\":\"task-1\",\"certainty\":\"confirmed\",\"status\":\"open\",\"due_date\":\"2026-11-09\",\"assignee_keys\":[\"담당자\"],\"reason\":\"근거와 일치\",\"evidence_refs\":[\"task-1\"]}"}]}]}`), nil
		case path == "/api/v10/channels/333333333333333333/messages" && req.Method == "POST":
			posts++
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			posted = body.Content
			return response(`{"id":"666666666666666666"}`), nil
		default:
			t.Fatalf("unexpected request %s %s", req.Method, path)
			return nil, nil
		}
	})
	cfg := Config{NotionToken: "notion-test", DiscordToken: "discord-test", OpenRouterKey: "openrouter-test", OpenRouterModel: "test-model", MeetingDataSourceID: "meetings", SourceChannelID: "222222222222222222", ReminderChannelID: "333333333333333333", UserMap: map[string]string{"담당자": "777777777777777777"}, Now: now}
	cfg.DryRun = true
	preview, err := runWithAPI(context.Background(), cfg, api)
	if err != nil || preview.Eligible != 1 || preview.Sent != 0 || posts != 0 {
		t.Fatalf("dry run: %+v %v posts=%d", preview, err, posts)
	}
	cfg.DryRun = false
	first, err := runWithAPI(context.Background(), cfg, api)
	if err != nil || first.Sent != 1 || posts != 1 {
		t.Fatalf("first run: %+v %v posts=%d", first, err, posts)
	}
	if !strings.Contains(posted, "https://discord.com/channels/444444444444444444/555555555555555555") {
		t.Fatal("thread URL omitted")
	}
	second, err := runWithAPI(context.Background(), cfg, api)
	if err != nil || second.AlreadySent != 1 || second.Sent != 0 || posts != 1 {
		t.Fatalf("second run: %+v %v posts=%d", second, err, posts)
	}
}

func TestEmbeddedOperationsMemberMap(t *testing.T) {
	var members map[string]string
	if err := json.Unmarshal(defaultUserMapJSON, &members); err != nil {
		t.Fatal(err)
	}
	if len(members) != 9 {
		t.Fatalf("expected 9 operations members, got %d", len(members))
	}
	if members["김찬우"] != "345592083546898432" || members["윤시헌"] != "1324062125450330215" {
		t.Fatal("member IDs do not match the requested list")
	}
	for name, id := range members {
		if name == "" || !snowflake(id) {
			t.Fatalf("invalid member mapping %q: %q", name, id)
		}
	}
}

func TestDoneReactionOnThreadStarterOrMessage(t *testing.T) {
	for _, tc := range []struct {
		name           string
		starter        string
		threadMessages string
		wantDone       bool
	}{
		{"starter", `{"id":"555555555555555555","reactions":[{"count":1,"emoji":{"id":"1501195973865640040"}}]}`, `[]`, true},
		{"thread message without text", `{"id":"555555555555555555"}`, `[{"id":"666666666666666666","channel_id":"555555555555555555","content":"","timestamp":"2026-11-08T00:00:00Z","author":{"id":"777777777777777777"},"reactions":[{"count":1,"emoji":{"id":"1501195973865640040"}}]}]`, true},
		{"thread starter inside thread", "404", `[{"id":"555555555555555555","channel_id":"555555555555555555","content":"","timestamp":"2026-11-08T00:00:00Z","reactions":[{"count":1,"emoji":{"id":"1501195973865640040"}}]}]`, true},
		{"different emoji", `{"id":"555555555555555555","reactions":[{"count":1,"emoji":{"id":"999999999999999999"}}]}`, `[]`, false},
		{"removed reaction", `{"id":"555555555555555555","reactions":[{"count":0,"emoji":{"id":"1501195973865640040"}}]}`, `[]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newAPIClient()
			api.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/api/v10/channels/555555555555555555":
					return response(`{"id":"555555555555555555","guild_id":"444444444444444444","parent_id":"222222222222222222"}`), nil
				case "/api/v10/channels/222222222222222222/messages/555555555555555555":
					if tc.starter == "404" {
						return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
					}
					return response(tc.starter), nil
				case "/api/v10/channels/555555555555555555/messages":
					return response(tc.threadMessages), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			})
			candidates := []Candidate{{ID: "task-1", ThreadIDs: []string{"555555555555555555"}}}
			d := discordClient{api: api, token: "test", guildID: "444444444444444444", now: time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)}
			if err := d.attachThreadEvidence(context.Background(), candidates); err != nil {
				t.Fatal(err)
			}
			if candidates[0].DoneReaction != tc.wantDone {
				t.Fatalf("DoneReaction=%t, want %t", candidates[0].DoneReaction, tc.wantDone)
			}
			if tc.wantDone {
				if _, reason := validDecision(candidates[0], Decision{Status: "open"}, nil, "2026-11-09"); reason != "Discord Done reaction" {
					t.Fatalf("Done reaction did not block reminder: %s", reason)
				}
			}
		})
	}
}

func TestDigestMentionsConfirmedOnly(t *testing.T) {
	d := discordClient{}
	confirmed := []Reminder{{Title: "공지 작성", DueDate: "2026-11-09", NotionURL: "https://notion.so/one", UserIDs: []string{"123456789012345678"}}}
	uncertain := []UncertainItem{{Title: "<@123456789012345678> 일정 확인", DueDate: "2026-11-09", NotionURL: "https://notion.so/two", Reason: "담당자 재확인 필요"}}
	content, ids, err := d.formatDigest("2026-11-09", confirmed, uncertain, false, 2, 2)
	if err != nil || len(ids) != 1 || ids[0] != "123456789012345678" {
		t.Fatalf("digest: %q %#v %v", content, ids, err)
	}
	if strings.Count(content, "<@123456789012345678>") != 1 || !strings.Contains(content, "❔ 확인 필요 · 멘션 없음") {
		t.Fatalf("uncertain item mentioned a user or was missing: %q", content)
	}
}

func TestMeetingFollowupSendsOnlyVerifiedOpenTask(t *testing.T) {
	now := time.Date(2026, 11, 8, 9, 0, 0, 0, time.FixedZone("KST", 9*3600))
	if got := previousSaturday(now).Format("2006-01-02"); got != "2026-11-07" {
		t.Fatalf("previous Saturday = %s", got)
	}
	posts := 0
	modelCalls := 0
	api := newAPIClient()
	api.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "openrouter.ai":
			modelCalls++
			result := map[string]any{"candidate_id": "open-task", "status": "open", "due_date": nil, "assignee_keys": []string{"담당자"}, "reason": "회의에서 맡음", "evidence_refs": []string{"open-task"}}
			if modelCalls == 2 {
				result["certainty"] = "confirmed"
			}
			if modelCalls >= 3 {
				result["candidate_id"] = "done-task"
				result["status"] = "done"
				result["evidence_refs"] = []string{"done-task"}
				if modelCalls == 4 {
					result["certainty"] = "confirmed"
				}
			}
			inner, _ := json.Marshal(result)
			outer, _ := json.Marshal(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": string(inner)}}}}})
			return response(string(outer)), nil
		case req.URL.Path == "/api/v10/channels/333333333333333333/messages" && req.Method == "GET":
			return response(`[]`), nil
		case req.URL.Path == "/api/v10/channels/333333333333333333/messages" && req.Method == "POST":
			posts++
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body.Content, "<@777777777777777777>") || strings.Contains(body.Content, "done-task") || !strings.Contains(body.Content, "기한: 미정") {
				t.Fatalf("wrong followup content: %s", body.Content)
			}
			return response(`{"id":"666666666666666666"}`), nil
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
			return nil, nil
		}
	})
	candidates := []Candidate{
		{ID: "open-task", Title: "담당자 공지 작성", URL: "https://notion.so/open-task", AssigneeKeys: []string{"담당자"}, Evidence: []Evidence{{Ref: "open-task", Text: "담당자 공지 작성", At: now}}},
		{ID: "done-task", Title: "완료된 일", URL: "https://notion.so/done-task", Evidence: []Evidence{{Ref: "done-task", Text: "완료", At: now}}},
		{ID: "reaction-task", DoneReaction: true},
	}
	client := discordClient{api: api, token: "discord", botID: "111111111111111111", guildID: "444444444444444444", reminderID: "333333333333333333", now: now}
	model := openAIClient{api: api, key: "key", model: "model", now: now, mode: "meeting-followup"}
	cfg := Config{UserMap: map[string]string{"담당자": "777777777777777777"}}
	result, err := runMeetingFollowup(context.Background(), cfg, client, model, candidates, []string{"담당자"}, "2026-11-07", Result{})
	if err != nil || result.Eligible != 1 || result.Sent != 1 || posts != 1 || modelCalls != 4 {
		t.Fatalf("followup result=%+v err=%v posts=%d modelCalls=%d", result, err, posts, modelCalls)
	}
}
