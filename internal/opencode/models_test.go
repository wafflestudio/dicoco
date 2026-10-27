package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const modelFixture = `{"data":[
 {"id":"one","providerID":"opencode","name":"One Free","enabled":true,"status":"active","capabilities":{"tools":true},"cost":[{"input":0,"output":0}],"settings":{"apiKey":"never-display-this"}},
 {"id":"two","providerID":"opencode","name":"Two Free","enabled":true,"status":"active","capabilities":{"tools":true},"cost":[{"input":0,"output":0}]},
 {"id":"paid","providerID":"opencode","enabled":true,"status":"active","capabilities":{"tools":true},"cost":[{"input":0,"output":1}]},
 {"id":"unknown-price","providerID":"opencode","enabled":true,"status":"active","capabilities":{"tools":true},"cost":[{}]},
 {"id":"disabled","providerID":"opencode","enabled":false,"status":"active","capabilities":{"tools":true},"cost":[{"input":0,"output":0}]},
 {"id":"tiered-paid","providerID":"opencode","enabled":true,"status":"active","capabilities":{"tools":true},"cost":[{"input":0,"output":0},{"input":1,"output":1}]},
 {"id":"no-tools","providerID":"opencode","enabled":true,"status":"active","cost":[{"input":0,"output":0}]}
]}`

func TestGlobalModelAcrossExistingSessions(t *testing.T) {
	var events []string
	failSwitch := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/model":
			if r.URL.Query().Get("location[directory]") != "/workspace" {
				t.Error("missing location-scoped catalog")
			}
			w.Write([]byte(modelFixture))
		case r.URL.Path == "/api/model/default":
			w.Write([]byte(`{"data":{"id":"one","providerID":"opencode","name":"One Free"}}`))
		case strings.HasSuffix(r.URL.Path, "/model"):
			var body struct {
				Model struct{ ID, ProviderID string }
			}
			json.NewDecoder(r.Body).Decode(&body)
			events = append(events, r.URL.Path+":"+body.Model.ProviderID+"/"+body.Model.ID)
			if failSwitch {
				w.WriteHeader(400)
			} else {
				w.WriteHeader(204)
			}
		case strings.HasSuffix(r.URL.Path, "/prompt"):
			events = append(events, "prompt")
			w.Write([]byte(`{}`))
		case strings.HasSuffix(r.URL.Path, "/wait"):
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/message"):
			w.Write([]byte(`{"data":[{"type":"idle","outcome":"succeeded"},{"type":"assistant","finish":"stop","content":[{"type":"text","text":"done"}]},{"id":"msg_discord_request","type":"user"}]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "", "/workspace")
	ctx := context.Background()
	models, err := c.Models(ctx)
	if err != nil || len(models) != 2 {
		t.Fatalf("free models: %v %v", models, err)
	}
	encoded, _ := json.Marshal(models)
	if strings.Contains(string(encoded), "never-display-this") {
		t.Fatal("provider credentials leaked into display metadata")
	}
	current, err := c.CurrentModel(ctx)
	if err != nil || !strings.Contains(current, "opencode/one") {
		t.Fatalf("default model: %q %v", current, err)
	}
	if _, err := c.SelectModel(ctx, "Two Free"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"existing-thread", "existing-dm"} {
		if _, err := c.Ask(ctx, id, "request", "hello"); err != nil {
			t.Fatal(err)
		}
	}
	want := "/api/session/existing-thread/model:opencode/two,prompt,/api/session/existing-dm/model:opencode/two,prompt"
	if strings.Join(events, ",") != want {
		t.Fatalf("global model was not applied before prompts: %v", events)
	}
	if _, err := c.SelectModel(ctx, "paid"); !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("paid selection accepted: %v", err)
	}
	current, _ = c.CurrentModel(ctx)
	if current != "opencode/two" {
		t.Fatalf("failed selection changed current model: %s", current)
	}
	failSwitch = true
	if _, err := c.Ask(ctx, "existing-thread", "request", "hello"); err == nil {
		t.Fatal("model switch failure silently fell back")
	}
	if events[len(events)-1] == "prompt" {
		t.Fatal("prompt admitted after failed switch")
	}
}

func TestLiveModelSelection(t *testing.T) {
	if os.Getenv("DICOCO_OPENCODE_MODEL_TEST") != "1" {
		t.Skip("set DICOCO_OPENCODE_MODEL_TEST=1; no inference calls")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, stop, err := Start(ctx, "opencode", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	models, err := c.Models(ctx)
	if err != nil || len(models) == 0 {
		t.Fatalf("actual model catalog: %d models, %v", len(models), err)
	}
	if _, err := c.CurrentModel(ctx); err != nil {
		t.Fatal(err)
	}
	selected, err := c.SelectModel(ctx, models[0].String())
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.Create(ctx, "Global model API compatibility check")
	if err != nil {
		t.Fatal(err)
	}
	defer c.request(ctx, "DELETE", "/api/session/"+id, nil, nil)
	if err := c.request(ctx, "POST", "/api/session/"+id+"/model", map[string]any{"model": map[string]string{"id": selected.ID, "providerID": selected.ProviderID}}, nil); err != nil {
		t.Fatal(err)
	}
	var response struct{ Data struct{ Model Model } }
	if err := c.request(ctx, "GET", "/api/session/"+id, nil, &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Model.ID != selected.ID || response.Data.Model.ProviderID != selected.ProviderID {
		t.Fatalf("model selection was not applied: %+v", response.Data.Model)
	}
	t.Logf("actual API: %d free models; session model selection verified without inference", len(models))
}
