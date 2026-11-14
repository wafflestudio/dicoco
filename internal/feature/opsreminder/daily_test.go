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

func TestMeetingPairUsesDateRatherThanCreatedTime(t *testing.T) {
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.FixedZone("KST", 9*3600))
	api := newAPIClient()
	api.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/data_sources/meetings/query" {
			t.Fatalf("unexpected request %s", req.URL)
			return nil, nil
		}
		return response(`{"results":[{"id":"old","created_time":"2026-10-10T00:00:00Z","last_edited_time":"2026-11-07T13:00:00Z","properties":{"Date":{"date":{"start":"2026-11-07"}}}},{"id":"next","created_time":"2026-10-10T00:00:00Z","properties":{"Date":{"date":{"start":"2026-11-20"}}}},{"id":"later","created_time":"2026-09-06T00:00:00Z","properties":{"Date":{"date":{"start":"2026-11-28"}}}}],"has_more":false}`), nil
	})
	old, next, err := (notionCollector{api: api, meetingID: "meetings", now: now}).meetingPair(context.Background())
	if err != nil || old.Page.ID != "old" || next.Page.ID != "next" {
		t.Fatalf("pair old=%+v next=%+v err=%v", old, next, err)
	}
}

func TestDailyPipelineWritesOnceAndRemindsOnlyOpenTask(t *testing.T) {
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.FixedZone("KST", 9*3600))
	sourceID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	sourceBlockID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	targetID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	conclusionID := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	headingID := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	groupID := "ffffffff-ffff-ffff-ffff-ffffffffffff"
	taskID := "11111111-1111-1111-1111-111111111111"
	sourceURL := "https://notion.so/source#" + strings.ReplaceAll(sourceBlockID, "-", "")
	headingExists := false
	taskExists := false
	posted := ""
	posts := 0
	checks := 0
	writes := 0
	api := newAPIClient()
	api.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		path := req.URL.Path
		switch {
		case path == "/v1/data_sources/meetings/query":
			return response(`{"results":[{"id":"` + sourceID + `","url":"https://notion.so/source","last_edited_time":"2026-11-07T13:10:00Z","properties":{"Date":{"date":{"start":"2026-11-07"}}}},{"id":"` + targetID + `","url":"https://notion.so/next","last_edited_time":"2026-11-12T00:00:00Z","properties":{"Date":{"date":{"start":"2026-11-20"}}}}],"has_more":false}`), nil
		case path == "/v1/blocks/"+targetID+"/children" && req.Method == "GET":
			blocks := `{"id":"` + conclusionID + `","type":"heading_2","heading_2":{"rich_text":[{"plain_text":"결론"}]}}`
			if headingExists {
				blocks += `,{"id":"` + headingID + `","type":"heading_2","has_children":true,"heading_2":{"rich_text":[{"plain_text":"TODO"}],"is_toggleable":true}}`
			}
			return response(`{"results":[` + blocks + `],"has_more":false}`), nil
		case path == "/v1/blocks/"+targetID+"/children" && req.Method == "PATCH":
			body, _ := io.ReadAll(req.Body)
			if !strings.Contains(string(body), `"is_toggleable":true`) || !strings.Contains(string(body), `"after_block"`) {
				t.Fatalf("wrong TODO heading payload: %s", body)
			}
			writes++
			headingExists = true
			return response(`{"results":[{"id":"` + headingID + `","type":"heading_2"}]}`), nil
		case path == "/v1/blocks/"+sourceID+"/children":
			return response(`{"results":[{"id":"22222222-2222-2222-2222-222222222222","type":"heading_2","has_children":true,"heading_2":{"rich_text":[{"plain_text":"안건"}]}}],"has_more":false}`), nil
		case path == "/v1/blocks/22222222-2222-2222-2222-222222222222/children":
			return response(`{"results":[{"id":"` + sourceBlockID + `","type":"to_do","last_edited_time":"2026-11-07T13:10:00Z","to_do":{"rich_text":[{"plain_text":"공지 작성 @담당자","mention":{"type":"user","user":{"name":"담당자"}}}],"checked":false}}],"has_more":false}`), nil
		case path == "/v1/blocks/"+headingID+"/children" && req.Method == "GET":
			if !taskExists {
				return response(`{"results":[],"has_more":false}`), nil
			}
			return response(`{"results":[{"id":"` + groupID + `","type":"bulleted_list_item","has_children":true,"bulleted_list_item":{"rich_text":[{"plain_text":"담당자"}]}}],"has_more":false}`), nil
		case path == "/v1/blocks/"+headingID+"/children" && req.Method == "PATCH":
			body, _ := io.ReadAll(req.Body)
			if !strings.Contains(string(body), defaultDeadlineText) || !strings.Contains(string(body), sourceURL) || !strings.Contains(string(body), `"to_do"`) {
				t.Fatalf("wrong nested TODO payload: %s", body)
			}
			writes++
			taskExists = true
			return response(`{"results":[{"id":"` + groupID + `","type":"bulleted_list_item"}]}`), nil
		case path == "/v1/blocks/"+groupID+"/children":
			return response(`{"results":[{"id":"` + taskID + `","type":"to_do","last_edited_time":"2026-11-19T00:00:00Z","to_do":{"rich_text":[{"plain_text":"공지 작성 · 기한: ` + defaultDeadlineText + ` · "},{"plain_text":"출처","href":"` + sourceURL + `"}],"checked":false}}],"has_more":false}`), nil
		case path == "/v1/blocks/"+strings.ReplaceAll(sourceBlockID, "-", ""):
			return response(`{"id":"` + sourceBlockID + `","type":"to_do","last_edited_time":"2026-11-07T13:10:00Z","to_do":{"rich_text":[{"plain_text":"공지 작성 @담당자"}],"checked":false}}`), nil
		case path == "/api/v10/users/@me":
			return response(`{"id":"999999999999999999"}`), nil
		case path == "/api/v10/channels/222222222222222222":
			return response(`{"id":"222222222222222222","guild_id":"444444444444444444"}`), nil
		case path == "/api/v10/channels/333333333333333333":
			return response(`{"id":"333333333333333333","guild_id":"444444444444444444"}`), nil
		case path == "/api/v10/channels/222222222222222222/messages":
			return response(`[]`), nil
		case path == "/api/v10/channels/333333333333333333/messages" && req.Method == "GET":
			if posted == "" {
				return response(`[]`), nil
			}
			b, _ := json.Marshal([]map[string]any{{"id": "888888888888888888", "content": posted, "timestamp": "2026-11-19T01:00:00Z", "author": map[string]any{"id": "999999999999999999"}}})
			return response(string(b)), nil
		case path == "/api/v10/channels/333333333333333333/messages" && req.Method == "POST":
			posts++
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			posted = body.Content
			return response(`{"id":"888888888888888888"}`), nil
		case path == "/api/v1/responses":
			body, _ := io.ReadAll(req.Body)
			id := sourceBlockID
			status := "open"
			certainty := ""
			if strings.Contains(string(body), "daily-status") {
				t.Fatal("mode name leaked into API request")
			}
			if strings.Contains(string(body), "독립 검증자") {
				certainty = `"certainty":"confirmed",`
			}
			if strings.Contains(string(body), "완료 판정기") || strings.Contains(string(body), "완료 상태의 독립 검증자") {
				id = taskID
			}
			inner := `{"candidate_id":"` + id + `","task_title":"공지 작성",` + certainty + `"status":"` + status + `","due_date":null,"assignee_keys":["담당자"],"reason":"미완료","evidence_refs":["` + id + `"]}`
			outer, _ := json.Marshal(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": inner}}}}})
			return response(string(outer)), nil
		case path == "/v1/blocks/"+taskID && req.Method == "PATCH":
			checks++
			return response(`{}`), nil
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
			return nil, nil
		}
	})
	cfg := Config{NotionToken: "notion", DiscordToken: "discord", OpenRouterKey: "openrouter", OpenRouterModel: "test", MeetingDataSourceID: "meetings", SourceChannelID: "222222222222222222", ReminderChannelID: "333333333333333333", UserMap: map[string]string{"담당자": "777777777777777777"}, Now: now, Mode: "daily"}
	first, err := runWithAPI(context.Background(), cfg, api)
	if err != nil || first.CreatedTasks != 1 || first.Sent != 1 || posts != 1 || writes != 2 || checks != 0 {
		t.Fatalf("first=%+v err=%v posts=%d writes=%d checks=%d", first, err, posts, writes, checks)
	}
	if !strings.Contains(posted, "기한: 다음 회의 전 (2026-11-20)") || !strings.Contains(posted, "<@777777777777777777>") || strings.Contains(posted, "근거:") {
		t.Fatalf("wrong reminder %q", posted)
	}
	second, err := runWithAPI(context.Background(), cfg, api)
	if err != nil || second.CreatedTasks != 0 || second.AlreadySent != 1 || posts != 1 || writes != 2 {
		t.Fatalf("second=%+v err=%v posts=%d writes=%d", second, err, posts, writes)
	}
}

func TestDailyReminderTriggersOnMeetingOrDeadlineEve(t *testing.T) {
	task := managedTask{Title: "작업", DueDate: "2026-11-20", URL: "https://notion.so/next#task", SourceURL: "https://notion.so/source#task"}
	msg, err := formatDailyReminder("2026-11-20", "2026-11-20", task, nil)
	if err != nil || !strings.Contains(msg, "[작업](https://notion.so/next#task)") || strings.Contains(msg, "근거:") {
		t.Fatalf("combined trigger %q %v", msg, err)
	}
	if _, err := formatDailyReminder("2026-11-19", "2026-11-20", task, nil); err == nil {
		t.Fatal("non-trigger day accepted")
	}
}

func TestCompletionNeedsLinkedDiscordEvidence(t *testing.T) {
	c := Candidate{Evidence: []Evidence{{Ref: "source", Kind: "source_meeting"}, {Ref: "message", Kind: "discord_thread_message"}}}
	v := Verification{Certainty: "confirmed", Status: "done", EvidenceRefs: []string{"source"}}
	if verifiedClosedFromThread(c, v) {
		t.Fatal("source note alone closed task")
	}
	v.EvidenceRefs = []string{"message"}
	if !verifiedClosedFromThread(c, v) {
		t.Fatal("linked Discord completion ignored")
	}
	v.Certainty = "uncertain"
	if verifiedClosedFromThread(c, v) {
		t.Fatal("uncertain completion closed task")
	}
}

func TestDefaultDeadlineFollowsMeetingDate(t *testing.T) {
	initial := managedTask{Title: "민상렬홀 사용 전 방문", DueDate: "2026-11-20", DefaultDeadline: true, SourceURL: "https://notion.so/source#block"}
	block := todoBlock(initial)
	data, err := json.Marshal(block["to_do"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), defaultDeadlineText) {
		t.Fatal("default deadline guidance missing from Notion write")
	}
	data, err = json.Marshal(map[string]any{"rich_text": []map[string]any{
		{"plain_text": initial.Title + " · 기한: " + defaultDeadlineText + " · "},
		{"plain_text": "출처", "href": initial.SourceURL},
	}, "checked": false})
	if err != nil {
		t.Fatal(err)
	}
	b := notionBlock{ID: "task", Type: "to_do", Data: map[string]json.RawMessage{"to_do": data}}
	for _, date := range []string{"2026-11-20", "2026-11-28"} {
		parsed, err := parseManagedTask(b, "담당 미정", "https://notion.so/next", date)
		if err != nil || !parsed.DefaultDeadline || parsed.DueDate != date {
			t.Fatalf("date=%s parsed=%+v err=%v", date, parsed, err)
		}
	}
}
