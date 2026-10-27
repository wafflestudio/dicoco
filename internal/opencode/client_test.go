package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTurnProtocol(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "opencode" || password != "test" {
			t.Error("missing auth")
		}
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/info":
			w.Write([]byte(`{"version":"2.1.4"}`))
		case "/api/session":
			var body struct {
				Permissions []rule `json:"permissions"`
				Model       any    `json:"model"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Permissions) == 0 || body.Permissions[0].Effect != "deny" || body.Model != nil {
				t.Error("expected default model and restricted session")
			}
			w.Write([]byte(`{"data":{"id":"ses_test"}}`))
		case "/api/experimental/session/ses_test/instructions/entries/discord":
			w.WriteHeader(204)
		case "/api/session/ses_test/prompt":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["delivery"] != "queue" || body["id"] != "msg_discord_42" {
				t.Errorf("unexpected prompt %v", body)
			}
			w.Write([]byte(`{"data":{"id":"msg_discord_42"}}`))
		case "/api/experimental/session/ses_test/wait":
			w.WriteHeader(204)
		case "/api/session/ses_test/message":
			w.Write([]byte(`{"data":[{"id":"idle","type":"idle","outcome":"succeeded"},{"id":"answer","type":"assistant","finish":"stop","content":[{"type":"reasoning","text":"private reasoning"},{"type":"text","text":"확인 결과"}]},{"id":"msg_discord_42","type":"user"}],"cursor":{"next":null}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "test", "/workspace")
	if err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	id, err := c.Create(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	answer, err := c.Ask(context.Background(), id, "42", "question")
	if err != nil || answer != "확인 결과" {
		t.Fatalf("answer=%q error=%v", answer, err)
	}
	if len(requests) != 6 {
		t.Fatalf("unexpected calls %v", requests)
	}
}

func TestAnswerRejectsFailedAndOldTurns(t *testing.T) {
	for _, body := range []string{
		`{"data":[{"type":"idle","outcome":"failed"},{"type":"assistant","content":[{"type":"text","text":"no bugs"}]},{"id":"input","type":"user"}],"cursor":{}}`,
		`{"data":[{"type":"idle","outcome":"succeeded"},{"type":"assistant","finish":"length","content":[{"type":"text","text":"partial"}]},{"id":"input","type":"user"}],"cursor":{}}`,
		`{"data":[{"type":"assistant","content":[{"type":"text","text":"old answer"}]}],"cursor":{}}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		c, _ := New(srv.URL, "", "/workspace")
		if answer, err := c.answer(context.Background(), "/api/session/ses_test", "input"); err == nil {
			t.Errorf("accepted failed turn: %s", answer)
		}
		srv.Close()
	}
}

func TestAnswerPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "next" {
			w.Write([]byte(`{"data":[{"id":"input","type":"user"}],"cursor":{}}`))
			return
		}
		w.Write([]byte(`{"data":[{"type":"idle","outcome":"succeeded"},{"type":"assistant","finish":"stop","content":[{"type":"text","text":"new"}]}],"cursor":{"next":"next"}}`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "", "/workspace")
	answer, err := c.answer(context.Background(), "/api/session/ses_test", "input")
	if err != nil || answer != "new" {
		t.Fatalf("%q %v", answer, err)
	}
}

func TestProviderErrorDiagnostics(t *testing.T) {
	err := analysisError(json.RawMessage(`{"type":"APIError","status":429,"message":"rate limited\nretry later","response":{"body":"do not log raw body"}}`), "analysis failed")
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") || strings.Contains(err.Error(), "do not log raw body") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("unsafe/missing diagnostics: %s", err)
	}
}

// Checks OpenCode's real permission evaluator without making a model call or
// running any gh command against GitHub.
func TestLiveGHPermissions(t *testing.T) {
	if os.Getenv("DICOCO_OPENCODE_PERMISSION_TEST") != "1" {
		t.Skip("set DICOCO_OPENCODE_PERMISSION_TEST=1 to check installed OpenCode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, stop, err := Start(ctx, "opencode", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	id, err := c.Create(ctx, "Dicoco gh permission test")
	if err != nil {
		t.Fatal(err)
	}
	defer c.request(ctx, "DELETE", "/api/session/"+id, nil, nil)
	for _, test := range []struct{ command, effect string }{
		{"gh pr view https://github.com/wafflestudio/dicoco/pull/1 --json headRefOid", "allow"},
		{"gh pr diff https://github.com/wafflestudio/dicoco/pull/1", "allow"},
		{"gh api --method GET repos/wafflestudio/dicoco/contents/README.md", "allow"},
		{"gh pr review https://github.com/wafflestudio/dicoco/pull/1 --comment --body example", "allow"},
		{"gh pr review https://github.com/wafflestudio/dicoco/pull/1 --comment --approve --body example", "deny"},
		{"gh api --method GET repos/wafflestudio/dicoco/issues --method=POST", "deny"},
		{"gh pr merge https://github.com/wafflestudio/dicoco/pull/1", "deny"},
		{"gh auth token", "deny"},
		{"cat /var/run/secrets/github-app/private-key.pem", "deny"},
	} {
		var response struct {
			Data struct {
				Effect string `json:"effect"`
			} `json:"data"`
		}
		err := c.request(ctx, "POST", "/api/session/"+id+"/permission", map[string]any{"action": "shell", "resources": []string{test.command}}, &response)
		if err != nil || response.Data.Effect != test.effect {
			t.Fatalf("command=%s effect=%s want=%s error=%v", test.command, response.Data.Effect, test.effect, err)
		}
	}
	t.Log("installed OpenCode enforces gh allow/deny rules; no model or GitHub calls made")
}

// Opt-in integration test: starts the real installed OpenCode, exercises default
// free-model file reading and session context against a disposable fixture only.
func TestLiveOpenCode(t *testing.T) {
	if os.Getenv("DICOCO_OPENCODE_LIVE_TEST") != "1" {
		t.Skip("set DICOCO_OPENCODE_LIVE_TEST=1 for real model calls")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("The secret fruit is kumquat.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c, stop, err := Start(ctx, "opencode", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if err := c.Health(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := c.Create(ctx, "Dicoco integration smoke test")
	if err != nil {
		t.Fatal(err)
	}
	defer c.request(context.Background(), "DELETE", "/api/session/"+id, nil, nil)
	// Check the effective permissions, not just the configuration we sent.
	for _, action := range []string{"edit", "shell", "subagent", "external_directory"} {
		var response struct {
			Data struct {
				Effect string `json:"effect"`
			} `json:"data"`
		}
		err := c.request(ctx, "POST", "/api/session/"+id+"/permission", map[string]any{"action": action, "resources": []string{"*"}}, &response)
		if err != nil || response.Data.Effect != "deny" {
			t.Fatalf("effective permission %s=%s err=%v", action, response.Data.Effect, err)
		}
	}
	answer, err := c.Ask(ctx, id, "smoke_first", "Read sample.txt using the read tool and tell me the fruit it names. Do not run shell commands.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(answer), "kumquat") && !strings.Contains(answer, "금귤") {
		t.Fatalf("unexpected answer: %s", answer)
	}
	answer, err = c.Ask(ctx, id, "smoke_followup", "What fruit did you just find? Reply with only the English fruit name.")
	if err != nil || !strings.Contains(strings.ToLower(answer), "kumquat") {
		t.Fatalf("session context: %q %v", answer, err)
	}
	t.Log("OpenCode 2: startup, Basic auth, default model, read-only permissions, file reading, and follow-up session passed")
}
