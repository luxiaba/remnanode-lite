package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidatePostStartConfig(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{}`, `{"enabled":false}`, `{"enabled":true}`,
		`{"webhook":{"enabled":false,"url":"http://example.com/hook"}}`,
		`{"enabled":true,"webhook":{"enabled":true,"url":"https://example.com/hook?token=test"}}`,
	} {
		var section any
		if err := json.Unmarshal([]byte(raw), &section); err != nil {
			t.Fatal(err)
		}
		if err := ValidatePluginConfig(map[string]any{"postStart": section}); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`null`, `[]`, `{"enabled":null}`, `{"enabled":"true"}`, `{"webhook":null}`,
		`{"webhook":{}}`, `{"webhook":{"enabled":true}}`,
		`{"webhook":{"enabled":null,"url":"https://example.com"}}`,
		`{"webhook":{"enabled":false,"url":"ftp://example.com"}}`,
		`{"webhook":{"enabled":true,"url":"https://"}}`,
		`{"webhook":{"enabled":true,"url":"/relative"}}`,
	} {
		var section any
		if err := json.Unmarshal([]byte(raw), &section); err != nil {
			t.Fatal(err)
		}
		if err := ValidatePluginConfig(map[string]any{"postStart": section}); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func postStartPlugin(t *testing.T, url string, enabled bool) *SyncPlugin {
	t.Helper()
	return mustSyncPlugin(t, map[string]any{"uuid": "test", "name": "post-start", "config": map[string]any{
		"postStart": map[string]any{"enabled": enabled, "webhook": map[string]any{"enabled": true, "url": url}},
	}})
}

func TestPostStartWebhookPayloadAndPluginUpdates(t *testing.T) {
	t.Parallel()
	received := make(chan map[string]json.RawMessage, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Error("invalid webhook request")
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		received <- body
		w.WriteHeader(http.StatusServiceUnavailable) // Delivery is best-effort.
	}))
	defer server.Close()
	state := NewState()
	service, _ := newReadyService(t, state, nil)
	if !service.Sync(postStartPlugin(t, server.URL, true)).Accepted {
		t.Fatal("plugin sync rejected")
	}
	metadata := &NodeMetadata{Name: "test-node", UUID: "node-uuid", ID: 7, Tags: []string{"edge"}, CountryCode: "CN"}
	service.NotifyCoreStarted(metadata)
	metadata.Tags[0] = "changed"
	select {
	case body := <-received:
		if string(body["scope"]) != `"service"` || string(body["event"]) != `"service.core_started"` {
			t.Fatalf("event = %s", body)
		}
		var timestamp string
		if err := json.Unmarshal(body["timestamp"], &timestamp); err != nil {
			t.Fatal(err)
		}
		if _, err := time.Parse("2006-01-02T15:04:05.000Z", timestamp); err != nil {
			t.Fatal(err)
		}
		if string(body["metadata"]) != `{"name":"test-node","uuid":"node-uuid","id":7,"tags":["edge"],"countryCode":"CN"}` {
			t.Fatalf("metadata = %s", body["metadata"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("missing post-start event")
	}
	service.NotifyCoreStarted(nil)
	select {
	case body := <-received:
		if string(body["metadata"]) != `{}` {
			t.Fatalf("missing metadata = %s", body["metadata"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("missing metadata-free event")
	}
	if !service.Sync(postStartPlugin(t, server.URL, false)).Accepted {
		t.Fatal("disable rejected")
	}
	service.NotifyCoreStarted(nil)
	if !service.Sync(nil).Accepted {
		t.Fatal("clear rejected")
	}
	service.NotifyCoreStarted(nil)
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	service.NotifyCoreStarted(nil)
	if len(received) != 0 {
		t.Fatal("disabled or cleared plugin sent an event")
	}
}

func TestPostStartWorksWithoutNFTAndTracksURLChanges(t *testing.T) {
	t.Parallel()
	state := NewState()
	backend := &fakeFirewall{initialize: errNFTablesUnavailable}
	service := newServiceWithBackend(state, nil, nil, backend)
	defer service.Close()
	if err := service.Initialize(); err == nil {
		t.Fatal("expected unavailable nft backend")
	}
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received <- struct{}{}; w.WriteHeader(204) }))
	defer server.Close()
	if !service.Sync(postStartPlugin(t, "http://example.invalid/old", true)).Accepted {
		t.Fatal("degraded sync rejected")
	}
	if !service.Sync(postStartPlugin(t, server.URL, true)).Accepted {
		t.Fatal("URL update rejected")
	}
	service.NotifyCoreStarted(nil)
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("post-start incorrectly depends on nft or retained stale URL")
	}
}

func TestPostStartQueueAndMetadataAreBounded(t *testing.T) {
	t.Parallel()
	state := NewState()
	state.commitSnapshot(&pluginSnapshot{postStart: postStartSettings{enabled: true, webhookEnabled: true, url: "https://example.com"}})
	service := &Service{state: state, webhookQueue: make(chan queuedWebhook, 1)}
	service.NotifyCoreStarted(&NodeMetadata{Name: strings.Repeat("x", maxPostStartMetadataBytes+1)})
	if len(service.webhookQueue) != 0 {
		t.Fatal("oversized metadata was queued")
	}
	service.NotifyCoreStarted(nil)
	service.NotifyCoreStarted(nil)
	if len(service.webhookQueue) != 1 {
		t.Fatal("queue exceeded its bound")
	}
}

func TestPostStartCloseCancelsActiveDelivery(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	canceled := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		once.Do(func() { close(entered) })
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	service, _ := newReadyService(t, NewState(), nil)
	if !service.Sync(postStartPlugin(t, server.URL, true)).Accepted {
		t.Fatal("sync rejected")
	}
	service.NotifyCoreStarted(nil)
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("delivery did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.CloseContext(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-ctx.Done():
		t.Fatal("delivery survived service shutdown")
	}
}
