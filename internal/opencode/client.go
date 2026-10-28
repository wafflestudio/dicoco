// Package opencode talks to the OpenCode 2 HTTP API using the Go standard library.
package opencode

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wafflestudio/dicoco/internal/ghguard"
)

type Client struct {
	url, password, directory string
	http                     *http.Client
	modelMu                  sync.RWMutex
	selectedModel            Model
}

func New(baseURL, password, directory string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("OPENCODE_URL must be an HTTP(S) URL without credentials, query or fragment")
	}
	return &Client{url: strings.TrimRight(baseURL, "/"), password: password, directory: directory,
		http: &http.Client{Timeout: 11 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.password != "" {
		req.SetBasicAuth("opencode", c.password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("OpenCode request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Do not echo response bodies: these may contain credentials or source code.
		return fmt.Errorf("OpenCode %s %s: HTTP %d", method, strings.Split(path, "?")[0], resp.StatusCode)
	}
	if output == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return err
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(output)
}

func (c *Client) Health(ctx context.Context) error {
	var info struct {
		Version string `json:"version"`
	}
	if err := c.request(ctx, "GET", "/api/info", nil, &info); err != nil {
		return err
	}
	if !strings.HasPrefix(info.Version, "2.") {
		return fmt.Errorf("OpenCode 2 required, got %q", info.Version)
	}
	log.Printf("[agent] OpenCode version=%s", info.Version)
	return nil
}

type rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// Shell permissions and the container's gh argv guard are complementary.
// Neither is an OS sandbox. Never allow the real gh executable directly.
func agentRules() []rule {
	rules := []rule{{"*", "*", "deny"}}
	for _, action := range []string{"read", "glob", "grep", "webfetch", "websearch"} {
		rules = append(rules, rule{action, "*", "allow"})
	}
	for _, command := range []string{
		"jq *", "head *", "tail *", "cut *", "wc *",
		"rg *", "grep *", "cat *", "ls *", "pwd *", "tr *", "paste *", "column *",
		"echo *", "printf *", "basename *", "dirname *", "stat *", "du *",
		"git status *", "git diff *", "git log *", "git show *", "git ls-files *",
	} {
		rules = append(rules, rule{"shell", command, "allow"})
	}
	// uniq's second positional argument is an output file; only use stdin.
	for _, flags := range []string{"", " -c", " -d", " -u", " -i", " -ic", " -ci"} {
		rules = append(rules, rule{"shell", "uniq" + flags, "allow"})
	}
	// sort has file-writing and subprocess options, including abbreviated and
	// grouped forms. Permit common stdin pipeline forms by exact match only.
	for _, flags := range []string{"", " -r", " -n", " -nr", " -rn", " -u"} {
		rules = append(rules, rule{"shell", "sort" + flags, "allow"})
		for field := 1; field <= 20; field++ {
			rules = append(rules, rule{"shell", fmt.Sprintf("sort -k%d%s", field, flags), "allow"})
		}
	}
	for _, command := range ghguard.Commands {
		rules = append(rules, rule{"shell", "gh " + command + " *", "allow"})
	}
	rules = append(rules, rule{"shell", "gh --help", "allow"}, rule{"shell", "gh --version", "allow"})
	for _, command := range []string{
		"gh auth *", "gh repo delete *", "gh repo archive *", "gh repo rename *", "gh repo edit *",
		"gh repo deploy-key *", "gh secret *", "gh variable *", "gh pr merge *", "gh pr close *",
		"gh repo create *", "gh repo fork *", "gh repo sync *", "gh repo set-default *",
		"gh pr create *", "gh pr edit *", "gh pr reopen *", "gh pr ready *",
		"gh workflow run *", "gh workflow enable *", "gh workflow disable *",
		"gh run rerun *", "gh run cancel *", "gh run delete *", "gh release delete *",
		"gh pr review *--request-changes*", "gh pr review * -r*",
		"gh api *mutation*", "gh api *--input*", "gh api *@*",
		"gh api * -f*", "gh api * -F*", "gh api * --raw-field*", "gh api * --field*",
		"sort *--output*", "sort * -o*", "git diff *--output*",
		"rg *--pre*", "rg *--hostname*", "git *--ext*", "git *--text*", "git *--out*",
	} {
		rules = append(rules, rule{"shell", command, "deny"})
	}
	// Fields implicitly switch gh api to POST. Explicit GET queries
	// may use fields, but method overrides and mutations remain denied below.
	for _, command := range []string{"gh api --method GET *", "gh api --method=GET *", "gh api -X GET *", "gh api -XGET *"} {
		rules = append(rules, rule{"shell", command, "allow"})
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "post", "put", "patch", "delete"} {
		for _, flag := range []string{"--method ", "--method=", "-X ", "-X", "-X="} {
			rules = append(rules, rule{"shell", "gh api *" + flag + method + "*", "deny"})
		}
	}
	inlineComment := "gh api --method POST repos/wafflestudio/*/pulls/*/comments *"
	rules = append(rules, rule{"shell", inlineComment, "allow"})
	for _, flag := range []string{"--method", "-X", "--input", "--hostname"} {
		rules = append(rules, rule{"shell", inlineComment + flag + "*", "deny"})
	}
	for _, command := range []string{"gh api *mutation*", "gh api *graphql*", "gh api *--input*", "gh api *@*", "gh api *--hostname*", "gh api *--cache*"} {
		rules = append(rules, rule{"shell", command, "deny"})
	}
	for _, pattern := range []string{"*.pem*", "*.key*", "*.env*", "*/var/run/secrets/*", "*/.secrets/*", "*/.config/gh/*", "*/dicoco-gh-*", "*/proc/*"} {
		rules = append(rules, rule{"shell", pattern, "deny"})
	}
	for _, pattern := range []string{"*.env", "*.env.*", "*.pem", "*.key", ".secrets/*", "*/.secrets/*", ".git/*", "*/.git/*", "/var/run/secrets/*", "/proc/*", "*/dicoco-gh-*", "*/.config/gh/*"} {
		rules = append(rules, rule{"read", pattern, "deny"})
	}
	return rules
}

// instructions is embedded at build time and sent when each session is created.
// Edit agent.md to change the bot persona and default instructions.
//
//go:embed agent.md
var instructions string

func (c *Client) Create(ctx context.Context, title string) (string, error) {
	var response struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	input := map[string]any{"title": title, "location": map[string]string{"directory": c.directory}, "permissions": agentRules()}
	if err := c.request(ctx, "POST", "/api/session", input, &response); err != nil {
		return "", err
	}
	if response.Data.ID == "" {
		return "", errors.New("OpenCode returned an empty session ID")
	}
	path := "/api/experimental/session/" + url.PathEscape(response.Data.ID) + "/instructions/entries/discord"
	if err := c.request(ctx, "PUT", path, map[string]any{"value": instructions}, nil); err != nil {
		return "", err
	}
	return response.Data.ID, nil
}

// Ask only returns a completed, successful turn. A failed/partial turn is never
// represented as a successful "no findings" response.
func (c *Client) Ask(ctx context.Context, sessionID, requestID, text string) (string, error) {
	path := "/api/session/" + url.PathEscape(sessionID)
	c.modelMu.RLock()
	model := c.selectedModel
	c.modelMu.RUnlock()
	if model.ID != "" {
		if err := c.request(ctx, "POST", path+"/model", map[string]any{"model": map[string]string{"providerID": model.ProviderID, "id": model.ID}}, nil); err != nil {
			return "", err
		}
	}
	messageID := "msg_discord_" + requestID
	if err := c.request(ctx, "POST", path+"/prompt", map[string]any{"id": messageID, "text": text, "delivery": "queue"}, nil); err != nil {
		return "", err
	}
	if err := c.request(ctx, "POST", "/api/experimental/session/"+url.PathEscape(sessionID)+"/wait", nil, nil); err != nil {
		return "", err
	}
	return c.answer(ctx, path, messageID)
}

type message struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Outcome string          `json:"outcome"`
	Finish  string          `json:"finish"`
	Error   json.RawMessage `json:"error"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (c *Client) answer(ctx context.Context, path, inputID string) (string, error) {
	query := "?order=desc&limit=100"
	var messages []message
	found := false
	for page := 0; page < 10; page++ {
		var response struct {
			Data   []message `json:"data"`
			Cursor struct {
				Next string `json:"next"`
			} `json:"cursor"`
		}
		if err := c.request(ctx, "GET", path+"/message"+query, nil, &response); err != nil {
			return "", err
		}
		for _, m := range response.Data {
			if m.ID == inputID {
				found = true
				break
			}
			messages = append(messages, m)
		}
		if found || response.Cursor.Next == "" {
			break
		}
		query = "?limit=100&cursor=" + url.QueryEscape(response.Cursor.Next)
	}
	if !found {
		return "", errors.New("OpenCode input missing from completed turn")
	}
	idle := false
	answer := ""
	seenAssistant := false
	for _, m := range messages { // Newest first: retain the final answer, not tool commentary.
		if m.Type == "idle" {
			if m.Outcome != "succeeded" {
				return "", analysisError(m.Error, "OpenCode turn "+m.Outcome)
			}
			idle = true
		}
		if m.Type != "assistant" {
			continue
		}
		if (len(m.Error) > 0 && string(m.Error) != "null") || m.Finish == "error" || m.Finish == "length" || m.Finish == "content-filter" {
			return "", analysisError(m.Error, "OpenCode analysis failed or was incomplete")
		}
		if !seenAssistant {
			seenAssistant = true
			if m.Finish != "stop" {
				return "", errors.New("OpenCode final assistant message did not finish normally")
			}
			var parts []string
			for _, p := range m.Content {
				if p.Type == "text" {
					parts = append(parts, p.Text)
				}
			}
			answer = strings.TrimSpace(strings.Join(parts, "\n"))
		}
	}
	if !idle || answer == "" {
		return "", errors.New("OpenCode returned no completed answer")
	}
	return answer, nil
}

// Keep provider diagnostics in server logs; never return raw provider bodies to
// Discord. Quoting and bounding messages avoids multiline/unbounded log output.
func analysisError(raw json.RawMessage, fallback string) error {
	var detail struct {
		Type    string `json:"type"`
		Status  int    `json:"status"`
		Message string `json:"message"`
	}
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &detail) != nil {
		return errors.New(fallback)
	}
	message := []rune(detail.Message)
	if len(message) > 2000 {
		message = message[:2000]
	}
	return fmt.Errorf("%s: type=%q status=%d message=%q", fallback, detail.Type, detail.Status, string(message))
}

func (c *Client) Interrupt(ctx context.Context, sessionID string) error {
	return c.request(ctx, "POST", "/api/session/"+url.PathEscape(sessionID)+"/interrupt?resume=false", nil, nil)
}
