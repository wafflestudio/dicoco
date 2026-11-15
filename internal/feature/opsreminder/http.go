package opsreminder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type apiClient struct{ http *http.Client }

type apiHTTPError struct {
	Host       string
	StatusCode int
	Code       string
	Detail     string
}

func (e *apiHTTPError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s HTTP %d: %s", e.Host, e.StatusCode, e.Detail)
	}
	if e.Code != "" {
		return fmt.Sprintf("%s HTTP %d (%s)", e.Host, e.StatusCode, e.Code)
	}
	return fmt.Sprintf("%s HTTP %d", e.Host, e.StatusCode)
}

func newAPIClient() *apiClient { return &apiClient{http: &http.Client{Timeout: 90 * time.Second}} }

func (a *apiClient) request(ctx context.Context, method, endpoint string, headers map[string]string, input, output any, retryRead bool) error {
	var raw []byte
	if input != nil {
		var err error
		raw, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if input != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := a.http.Do(req)
		if err != nil {
			return fmt.Errorf("%s request failed: %w", req.URL.Host, err)
		}
		if retryRead && (resp.StatusCode == 429 || resp.StatusCode >= 500) && attempt < 2 {
			delay := time.Duration(attempt+1) * time.Second
			if s := resp.Header.Get("Retry-After"); s != "" {
				if n, e := strconv.Atoi(s); e == nil && n >= 0 {
					delay = time.Duration(n) * time.Second
				}
			}
			resp.Body.Close()
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			status := &apiHTTPError{Host: req.URL.Host, StatusCode: resp.StatusCode}
			var parsed struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(body, &parsed) == nil {
				status.Code = parsed.Error.Code
				if req.URL.Host == "openrouter.ai" && len(parsed.Error.Message) < 300 {
					status.Detail = parsed.Error.Message
				}
			}
			return status
		}
		if output == nil {
			resp.Body.Close()
			return nil
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(output)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("%s invalid JSON: %w", req.URL.Host, err)
		}
		return nil
	}
	return errors.New("request retry exhausted")
}

func notionHeaders(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token, "Notion-Version": "2025-09-03"}
}
func discordHeaders(token string) map[string]string {
	return map[string]string{"Authorization": "Bot " + token, "User-Agent": "DiscordBot (https://github.com/wafflestudio/dicoco, 1.0)"}
}
func openRouterHeaders(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}
func trimDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return strings.TrimSpace(s)
}
