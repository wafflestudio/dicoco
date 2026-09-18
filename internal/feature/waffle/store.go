package waffle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const stateVersion = 1
const maxStateSize = 1 << 20

type State struct {
	Version            int            `json:"version"`
	NextAnnouncementAt *time.Time     `json:"next_announcement_at,omitempty"`
	Recipients         map[string]int `json:"recipients"`
}

type stateStore interface {
	Load(context.Context) (State, error)
	Save(context.Context, State) error
}

type httpStateStore struct {
	url    string
	client *http.Client
}

func newHTTPStateStore(url string, client *http.Client) *httpStateStore {
	return &httpStateStore{url: url, client: client}
}

func (s *httpStateStore) Load(ctx context.Context) (State, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return State{}, fmt.Errorf("create GET request")
	}

	response, err := s.client.Do(request)
	if err != nil {
		return State{}, fmt.Errorf("send GET request failed")
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return State{}, fmt.Errorf("GET returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxStateSize+1))
	if err != nil {
		return State{}, fmt.Errorf("read GET response: %w", err)
	}
	if len(body) > maxStateSize {
		return State{}, fmt.Errorf("state exceeds %d bytes", maxStateSize)
	}

	var state State
	if err := json.Unmarshal(body, &state); err != nil {
		return State{}, fmt.Errorf("decode state JSON: %w", err)
	}

	return state, nil
}

func (s *httpStateStore) Save(ctx context.Context, state State) error {
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state JSON: %w", err)
	}
	body = append(body, '\n')

	request, err := http.NewRequestWithContext(ctx, http.MethodPut, s.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create PUT request")
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("send PUT request failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("PUT returned status %d", response.StatusCode)
	}

	return nil
}
