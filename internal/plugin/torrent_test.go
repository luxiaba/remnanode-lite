package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/xraywebhook"
)

type validatingFirewall struct {
	fakeFirewall
}

func (f *validatingFirewall) BlockIPs(ctx context.Context, items []BlockIP) error {
	if _, err := renderNFTBlock(items); err != nil {
		return err
	}
	return f.fakeFirewall.BlockIPs(ctx, items)
}

func TestExtractWebhookIP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		source string
		want   string
	}{
		{"tcp:203.0.113.10:443", "203.0.113.10"},
		{"udp:[2001:db8::1]:53", "2001:db8::1"},
		{"tcp:[2001:0db8:0000:0000:0000:0000:0000:0001]:443", "2001:db8::1"},
		{"203.0.113.20", "203.0.113.20"},
		{"invalid", ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			if got := extractWebhookIP(tc.source); got != tc.want {
				t.Fatalf("extractWebhookIP(%q) = %q, want %q", tc.source, got, tc.want)
			}
		})
	}
}

func TestTorrentIgnoreMatcherCanonicalizesAddressesAndCIDRs(t *testing.T) {
	t.Parallel()

	settings := torrentSettings{ignoredIPs: newIPMatcher([]string{
		"2001:0db8:0000:0000:0000:0000:0000:0001",
		"192.0.2.0/24",
	})}
	if !torrentIPIgnored(settings, "2001:db8::1") || !torrentIPIgnored(settings, "192.0.2.42") {
		t.Fatal("canonical IPv6 or CIDR ignore did not match")
	}
	if torrentIPIgnored(settings, "2001:db8::2") || torrentIPIgnored(settings, "198.51.100.1") {
		t.Fatal("ignore matcher matched an address outside its prefixes")
	}
	if !torrentIPIgnored(settings, "127.0.0.2") || !torrentIPIgnored(settings, "::1") {
		t.Fatal("protected loopback address was not ignored")
	}
}

func TestHandleXrayWebhookBlocksAndAddsReport(t *testing.T) {
	t.Parallel()

	state := NewState()
	service, backend := newReadyService(t, state, nil)
	if response := service.Sync(torrentPlugin(t, true, nil)); !response.Accepted {
		t.Fatal("torrent sync failed")
	}
	service.HandleXrayWebhook(xraywebhook.Payload{
		Email:       xraywebhook.String("user-1"),
		Source:      xraywebhook.String("tcp:203.0.113.10:443"),
		Network:     xraywebhook.String("tcp"),
		Destination: xraywebhook.String("198.51.100.1:443"),
		Timestamp:   xraywebhook.Number(123),
	})

	deadline := time.Now().Add(time.Second)
	for state.ReportsCount() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("reports count = %d, want 1", state.ReportsCount())
		}
		time.Sleep(time.Millisecond)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.blockCalls) != 1 || len(backend.blockCalls[0]) != 1 {
		t.Fatalf("block calls = %#v", backend.blockCalls)
	}
}

func TestHandleXrayWebhookPostsConfiguredReportWithoutChangingLocalOutcome(t *testing.T) {
	t.Parallel()

	type observation struct {
		method      string
		contentType string
		report      TorrentReport
		err         error
	}
	received := make(chan observation, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var report TorrentReport
		err := json.NewDecoder(request.Body).Decode(&report)
		received <- observation{
			method:      request.Method,
			contentType: request.Header.Get("Content-Type"),
			report:      report,
			err:         err,
		}
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	state := NewState()
	service, backend := newReadyService(t, state, nil)
	if response := service.Sync(torrentPluginWithWebhookURL(t, "  "+server.URL+"  ")); !response.Accepted {
		t.Fatal("torrent webhook config was rejected")
	}
	service.HandleXrayWebhook(xraywebhook.Payload{
		Email:       xraywebhook.String("user-1"),
		Network:     xraywebhook.String("tcp"),
		Source:      xraywebhook.String("tcp:203.0.113.10:443"),
		Destination: xraywebhook.String("198.51.100.1:443"),
		Timestamp:   xraywebhook.Number(123),
	})

	select {
	case got := <-received:
		if got.err != nil {
			t.Fatalf("decode posted report: %v", got.err)
		}
		if got.method != http.MethodPost || got.contentType != "application/json" {
			t.Fatalf("request method/content-type = %q/%q", got.method, got.contentType)
		}
		if got.report.ActionReport.UserID != "user-1" || got.report.ActionReport.IP != "203.0.113.10" || !got.report.ActionReport.Blocked {
			t.Fatalf("posted report = %#v", got.report)
		}
	case <-time.After(time.Second):
		t.Fatal("configured torrent report webhook was not called")
	}

	if state.ReportsCount() != 1 {
		t.Fatalf("local reports count = %d, want 1", state.ReportsCount())
	}
	backend.mu.Lock()
	blockCalls := len(backend.blockCalls)
	backend.mu.Unlock()
	if blockCalls != 1 {
		t.Fatalf("block calls = %d, want 1", blockCalls)
	}
}

func TestSlowTorrentReportWebhookDoesNotDelayLocalProcessing(t *testing.T) {
	t.Parallel()

	const reports = maxConcurrentTorrentWebhooks + 2
	requestsStarted := make(chan struct{}, reports)
	releaseRequests := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		requestsStarted <- struct{}{}
		select {
		case <-request.Context().Done():
		case <-releaseRequests:
		}
	}))
	t.Cleanup(func() {
		close(releaseRequests)
		server.Close()
	})

	state := NewState()
	service, backend := newReadyService(t, state, nil)
	if response := service.Sync(torrentPluginWithWebhookURL(t, server.URL)); !response.Accepted {
		t.Fatal("torrent webhook config was rejected")
	}
	payload := xraywebhook.Payload{
		Email:       xraywebhook.String("user-1"),
		Network:     xraywebhook.String("tcp"),
		Source:      xraywebhook.String("tcp:203.0.113.10:443"),
		Destination: xraywebhook.String("198.51.100.1:443"),
		Timestamp:   xraywebhook.Number(123),
	}
	service.HandleXrayWebhook(payload)
	select {
	case <-requestsStarted:
	case <-time.After(time.Second):
		t.Fatal("first torrent report webhook did not start")
	}

	for range reports - 1 {
		service.HandleXrayWebhook(payload)
	}
	deadline := time.Now().Add(2 * time.Second)
	for state.ReportsCount() != reports {
		if time.Now().After(deadline) {
			t.Fatalf("slow remote webhook delayed local reports: count=%d", state.ReportsCount())
		}
		time.Sleep(time.Millisecond)
	}
	remoteDeadline := time.Now().Add(time.Second)
	for len(requestsStarted) != maxConcurrentTorrentWebhooks-1 {
		if time.Now().After(remoteDeadline) {
			t.Fatalf("started remote webhooks = %d, want %d", len(requestsStarted)+1, maxConcurrentTorrentWebhooks)
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if got := len(requestsStarted) + 1; got != maxConcurrentTorrentWebhooks {
		t.Fatalf("concurrent remote webhooks = %d, want %d", got, maxConcurrentTorrentWebhooks)
	}
	backend.mu.Lock()
	blockCalls := len(backend.blockCalls)
	backend.mu.Unlock()
	if blockCalls != reports {
		t.Fatalf("block calls = %d, want %d", blockCalls, reports)
	}
}

func TestCloseCancelsInFlightTorrentReportWebhook(t *testing.T) {
	t.Parallel()

	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var startedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		startedOnce.Do(func() { close(requestStarted) })
		select {
		case <-request.Context().Done():
		case <-releaseRequest:
		}
	}))
	t.Cleanup(func() {
		close(releaseRequest)
		server.Close()
	})

	service, _ := newReadyService(t, NewState(), nil)
	if response := service.Sync(torrentPluginWithWebhookURL(t, server.URL)); !response.Accepted {
		t.Fatal("torrent webhook config was rejected")
	}
	service.HandleXrayWebhook(xraywebhook.Payload{
		Email:       xraywebhook.String("user-1"),
		Network:     xraywebhook.String("tcp"),
		Source:      xraywebhook.String("tcp:203.0.113.10:443"),
		Destination: xraywebhook.String("198.51.100.1:443"),
		Timestamp:   xraywebhook.Number(123),
	})
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("torrent report webhook did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	if err := service.CloseContext(ctx); err != nil {
		t.Fatalf("close service: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("close waited %s for canceled torrent report webhook", elapsed)
	}
}

func TestHandleXrayWebhookPreservesPermanentBlockSemantics(t *testing.T) {
	t.Parallel()

	state := NewState()
	service, backend := newReadyService(t, state, nil)
	if response := service.Sync(torrentPluginWithDuration(t, true, 0, nil)); !response.Accepted {
		t.Fatal("permanent torrent config was rejected")
	}
	service.HandleXrayWebhook(xraywebhook.Payload{
		Email:  xraywebhook.String("user-1"),
		Source: xraywebhook.String("tcp:203.0.113.10:443"),
	})

	deadline := time.Now().Add(time.Second)
	for state.ReportsCount() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("permanent torrent webhook was not processed")
		}
		time.Sleep(time.Millisecond)
	}
	backend.mu.Lock()
	if len(backend.blockCalls) != 1 || len(backend.blockCalls[0]) != 1 || backend.blockCalls[0][0].Timeout != 0 {
		calls := backend.blockCalls
		backend.mu.Unlock()
		t.Fatalf("permanent block calls = %#v", calls)
	}
	backend.mu.Unlock()

	reports := service.CollectReports().Reports
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want 1", len(reports))
	}
	action := reports[0].ActionReport
	if action.BlockDuration != 0 || !action.WillUnblockAt.Equal(action.ProcessedAt) {
		t.Fatalf("permanent report = %#v", action)
	}
}

func TestHandleXrayWebhookBlocksWithOfficialLongDuration(t *testing.T) {
	t.Parallel()

	const duration = 31 * 24 * 60 * 60
	state := NewState()
	backend := &validatingFirewall{}
	service := newServiceWithBackend(state, nil, nil, backend)
	if err := service.Initialize(); err != nil {
		t.Fatalf("initialize plugin service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	if response := service.Sync(torrentPluginWithDuration(t, true, duration, nil)); !response.Accepted {
		t.Fatal("long-duration torrent config was rejected")
	}
	service.HandleXrayWebhook(xraywebhook.Payload{
		Email:  xraywebhook.String("user-1"),
		Source: xraywebhook.String("tcp:203.0.113.10:443"),
	})

	deadline := time.Now().Add(time.Second)
	for state.ReportsCount() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("long-duration torrent webhook was not processed")
		}
		time.Sleep(time.Millisecond)
	}
	backend.mu.Lock()
	if len(backend.blockCalls) != 1 || len(backend.blockCalls[0]) != 1 || backend.blockCalls[0][0].Timeout != duration {
		calls := backend.blockCalls
		backend.mu.Unlock()
		t.Fatalf("long-duration block calls = %#v", calls)
	}
	backend.mu.Unlock()

	reports := service.CollectReports().Reports
	if len(reports) != 1 || !reports[0].ActionReport.Blocked || reports[0].ActionReport.BlockDuration != duration {
		t.Fatalf("long-duration reports = %#v", reports)
	}
}

func TestCloseDropsQueuedWebhookWithoutProcessingIt(t *testing.T) {
	t.Parallel()

	state := NewState()
	service, backend := newReadyService(t, state, nil)
	if response := service.Sync(torrentPlugin(t, true, nil)); !response.Accepted {
		t.Fatal("torrent sync failed")
	}
	service.operationGate <- struct{}{}
	for range 3 {
		service.HandleXrayWebhook(xraywebhook.Payload{
			Email:  xraywebhook.String("user-1"),
			Source: xraywebhook.String("tcp:203.0.113.10:443"),
		})
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- service.Close() }()
	deadline := time.Now().Add(time.Second)
	for !service.webhookStopped.Load() {
		if time.Now().After(deadline) {
			t.Fatal("Close did not stop webhook intake")
		}
		time.Sleep(time.Millisecond)
	}
	<-service.operationGate
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	blockCalls := len(backend.blockCalls)
	backend.mu.Unlock()
	if blockCalls != 0 || state.ReportsCount() != 0 {
		t.Fatalf("queued webhook ran during Close: blocks=%d reports=%d", blockCalls, state.ReportsCount())
	}
	if len(service.webhookQueue) != 0 {
		t.Fatalf("Close retained %d queued webhooks", len(service.webhookQueue))
	}
	service.HandleXrayWebhook(xraywebhook.Payload{})
	if len(service.webhookQueue) != 0 {
		t.Fatal("webhook was queued after Close")
	}
}

func TestWebhookStopWaitsForInFlightAdmission(t *testing.T) {
	t.Parallel()

	service := newServiceWithBackend(NewState(), nil, nil, &fakeFirewall{})
	t.Cleanup(func() { _ = service.Close() })
	service.webhookAdmissionMu.RLock()
	stopDone := make(chan struct{})
	go func() {
		service.signalWebhookStop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
		service.webhookAdmissionMu.RUnlock()
		t.Fatal("webhook stop crossed an in-flight admission")
	case <-time.After(20 * time.Millisecond):
	}
	service.webhookAdmissionMu.RUnlock()
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("webhook stop did not finish after admission released")
	}
	service.HandleXrayWebhook(xraywebhook.Payload{})
	if len(service.webhookQueue) != 0 {
		t.Fatal("webhook was queued after the admission fence closed")
	}
}

func TestHandleXrayWebhookQueuesWithoutWaitingForPluginMutation(t *testing.T) {
	t.Parallel()

	state := NewState()
	service, backend := newReadyService(t, state, nil)
	if response := service.Sync(torrentPlugin(t, true, nil)); !response.Accepted {
		t.Fatal("torrent sync failed")
	}
	applyStarted := make(chan struct{})
	releaseApply := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseApply) }) })
	backend.setApplyHook(func(call int) {
		if call == 2 {
			close(applyStarted)
			<-releaseApply
		}
	})
	syncDone := make(chan AcceptedResponse, 1)
	go func() { syncDone <- service.Sync(torrentAndFilterPlugin(t, "192.0.2.0/24")) }()
	select {
	case <-applyStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for plugin mutation")
	}

	started := time.Now()
	service.HandleXrayWebhook(xraywebhook.Payload{
		Email:  xraywebhook.String("user-1"),
		Source: xraywebhook.String("tcp:203.0.113.10:443"),
	})
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("webhook waited behind plugin mutation for %s", elapsed)
	}
	releaseOnce.Do(func() { close(releaseApply) })
	if response := <-syncDone; !response.Accepted {
		t.Fatal("sync was not accepted")
	}
	deadline := time.Now().Add(time.Second)
	for state.ReportsCount() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("queued webhook was not processed")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestHandleXrayWebhookWaitsForBoundedQueueCapacity(t *testing.T) {
	t.Parallel()

	service := newServiceWithBackend(NewState(), nil, nil, &fakeFirewall{})
	t.Cleanup(func() { _ = service.Close() })
	service.operationGate <- struct{}{}
	gateReleased := false
	t.Cleanup(func() {
		if !gateReleased {
			<-service.operationGate
		}
	})

	if !service.HandleXrayWebhook(xraywebhook.Payload{}) {
		t.Fatal("first webhook was not admitted")
	}
	deadline := time.Now().Add(time.Second)
	for len(service.webhookQueue) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("worker did not take first webhook")
		}
		time.Sleep(time.Millisecond)
	}
	for range cap(service.webhookQueue) {
		service.webhookQueue <- queuedWebhook{}
	}
	result := make(chan bool, 1)
	go func() {
		result <- service.HandleXrayWebhookContext(context.Background(), xraywebhook.Payload{})
	}()
	select {
	case <-result:
		t.Fatal("webhook admission returned while the bounded queue was full")
	case <-time.After(20 * time.Millisecond):
	}
	<-service.webhookQueue
	select {
	case accepted := <-result:
		if !accepted {
			t.Fatal("webhook was rejected after queue capacity became available")
		}
	case <-time.After(time.Second):
		t.Fatal("webhook admission did not resume after queue capacity became available")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if service.HandleXrayWebhookContext(canceled, xraywebhook.Payload{}) {
		t.Fatal("canceled webhook admission was accepted")
	}

	<-service.operationGate
	gateReleased = true
}

func TestWebhookStopWakesAdmissionWaitingOnFullQueue(t *testing.T) {
	t.Parallel()

	service := newServiceWithBackend(NewState(), nil, nil, &fakeFirewall{})
	service.operationGate <- struct{}{}
	gateReleased := false
	t.Cleanup(func() {
		if !gateReleased {
			<-service.operationGate
		}
		_ = service.Close()
	})

	if !service.HandleXrayWebhook(xraywebhook.Payload{}) {
		t.Fatal("first webhook was not admitted")
	}
	deadline := time.Now().Add(time.Second)
	for len(service.webhookQueue) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("worker did not take first webhook")
		}
		time.Sleep(time.Millisecond)
	}
	for range cap(service.webhookQueue) {
		service.webhookQueue <- queuedWebhook{}
	}

	admissionDone := make(chan bool, 1)
	go func() {
		admissionDone <- service.HandleXrayWebhookContext(context.Background(), xraywebhook.Payload{})
	}()
	select {
	case <-admissionDone:
		t.Fatal("webhook admission returned before shutdown")
	case <-time.After(20 * time.Millisecond):
	}

	stopDone := make(chan struct{})
	go func() {
		service.signalWebhookStop()
		close(stopDone)
	}()
	select {
	case accepted := <-admissionDone:
		if accepted {
			t.Fatal("shutdown accepted a webhook waiting on full capacity")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not wake webhook admission")
	}
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("webhook stop remained blocked behind admission")
	}

	<-service.operationGate
	gateReleased = true
}

func torrentPluginWithWebhookURL(t *testing.T, webhookURL string) *SyncPlugin {
	t.Helper()
	return mustSyncPlugin(t, map[string]any{
		"uuid": "00000000-0000-4000-8000-000000000001",
		"name": "test",
		"config": map[string]any{
			"torrentBlocker": map[string]any{
				"enabled":       true,
				"blockDuration": 300,
				"ignoreLists":   map[string]any{},
				"webhookUrl":    webhookURL,
			},
		},
	})
}
