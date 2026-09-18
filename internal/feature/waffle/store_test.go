package waffle

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestHTTPStateStoreLoadAndSave(t *testing.T) {
	state := State{
		Version: stateVersion,
		Recipients: map[string]int{
			"user-1": 1,
		},
	}

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.Method {
		case http.MethodGet:
			var body bytes.Buffer
			if err := json.NewEncoder(&body).Encode(state); err != nil {
				return nil, err
			}
			return responseWithBody(http.StatusOK, body.Bytes()), nil
		case http.MethodPut:
			if contentType := request.Header.Get("Content-Type"); contentType != "application/json" {
				t.Errorf("unexpected content type: %q", contentType)
			}
			if err := json.NewDecoder(request.Body).Decode(&state); err != nil {
				t.Errorf("decode request: %v", err)
			}
			return responseWithBody(http.StatusOK, nil), nil
		default:
			return responseWithBody(http.StatusMethodNotAllowed, nil), nil
		}
	})}

	store := newHTTPStateStore("https://example.invalid/state.json", client)
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Recipients["user-1"] != 1 {
		t.Fatalf("unexpected loaded state: %+v", loaded)
	}

	loaded.Recipients["user-1"] = 2
	if err := store.Save(context.Background(), loaded); err != nil {
		t.Fatal(err)
	}
	if state.Recipients["user-1"] != 2 {
		t.Fatalf("unexpected saved state: %+v", state)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func responseWithBody(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}
