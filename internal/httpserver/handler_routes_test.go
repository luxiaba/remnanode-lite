package httpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luxiaba/remnanode-lite/internal/connections"
	contractspec "github.com/luxiaba/remnanode-lite/internal/contract"
	"github.com/luxiaba/remnanode-lite/internal/nodehandler"
	"github.com/luxiaba/remnanode-lite/internal/xrayrpc"
)

type countingHandlerProvider struct {
	calls *atomic.Int64
}

func (p countingHandlerProvider) hit() { p.calls.Add(1) }

func (p countingHandlerProvider) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	p.hit()
	return ctx, func() {}, nil
}
func (p countingHandlerProvider) InboundTags() []string {
	p.hit()
	return []string{"inbound-1"}
}
func (p countingHandlerProvider) GetUserIPList(context.Context, string, bool) ([]xrayrpc.IPEntry, error) {
	p.hit()
	return []xrayrpc.IPEntry{}, nil
}
func (p countingHandlerProvider) HandlerRemoveUser(context.Context, string, string, string) xrayrpc.HandlerResult {
	p.hit()
	return xrayrpc.HandlerResult{OK: true}
}
func (p countingHandlerProvider) HandlerAddVlessUser(context.Context, string, string, string, string, uint32, string) xrayrpc.HandlerResult {
	p.hit()
	return xrayrpc.HandlerResult{OK: true}
}
func (p countingHandlerProvider) HandlerAddTrojanUser(context.Context, string, string, string, uint32, string) xrayrpc.HandlerResult {
	p.hit()
	return xrayrpc.HandlerResult{OK: true}
}
func (p countingHandlerProvider) HandlerAddShadowsocksUser(context.Context, string, string, string, int, bool, uint32, string) xrayrpc.HandlerResult {
	p.hit()
	return xrayrpc.HandlerResult{OK: true}
}
func (p countingHandlerProvider) HandlerAddShadowsocks2022User(context.Context, string, string, string, uint32, string) xrayrpc.HandlerResult {
	p.hit()
	return xrayrpc.HandlerResult{OK: true}
}
func (p countingHandlerProvider) HandlerAddHysteriaUser(context.Context, string, string, string, uint32, string) xrayrpc.HandlerResult {
	p.hit()
	return xrayrpc.HandlerResult{OK: true}
}
func (p countingHandlerProvider) HandlerGetInboundUsers(context.Context, string) ([]xrayrpc.InboundUser, xrayrpc.HandlerResult) {
	p.hit()
	return []xrayrpc.InboundUser{}, xrayrpc.HandlerResult{OK: true}
}
func (p countingHandlerProvider) HandlerGetInboundUsersCount(context.Context, string) (int64, xrayrpc.HandlerResult) {
	p.hit()
	return 1, xrayrpc.HandlerResult{OK: true}
}

type countingDropper struct {
	calls *atomic.Int64
}

type blockingHandlerProvider struct {
	countingHandlerProvider
	entered chan struct{}
	release <-chan struct{}
}

func (p *blockingHandlerProvider) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	close(p.entered)
	select {
	case <-p.release:
		return ctx, func() {}, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func (d countingDropper) DropIPs(context.Context, []string) bool {
	d.calls.Add(1)
	return true
}

func (d countingDropper) DropUsers(context.Context, connections.IPListProvider, []string) bool {
	d.calls.Add(1)
	return true
}

func TestHandlerValidationPrecedesAllSideEffects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "add user missing", path: "/node/handler/add-user", body: `{}`},
		{name: "add user unknown union", path: "/node/handler/add-user", body: `{"data":[{"type":"unknown"}],"hashData":{"vlessUuid":"00000000-0000-4000-8000-000000000001"}}`},
		{name: "add user invalid UUID", path: "/node/handler/add-user", body: `{"data":[],"hashData":{"vlessUuid":"bad"}}`},
		{name: "remove user missing", path: "/node/handler/remove-user", body: `{}`},
		{name: "inbound count missing", path: "/node/handler/get-inbound-users-count", body: `{}`},
		{name: "inbound users missing", path: "/node/handler/get-inbound-users", body: `{}`},
		{name: "add users missing", path: "/node/handler/add-users", body: `{}`},
		{name: "add users invalid nested UUID", path: "/node/handler/add-users", body: `{"affectedInboundTags":[],"users":[{"inboundData":[],"userData":{"userId":"u","hashUuid":"bad","vlessUuid":"bad","trojanPassword":"","ssPassword":""}}]}`},
		{name: "add users null affected inbound tag", path: "/node/handler/add-users", body: `{"affectedInboundTags":[null],"users":[]}`},
		{name: "remove users missing", path: "/node/handler/remove-users", body: `{}`},
		{name: "drop users empty", path: "/node/handler/drop-users-connections", body: `{"userIds":[]}`},
		{name: "drop users null item", path: "/node/handler/drop-users-connections", body: `{"userIds":[null]}`},
		{name: "drop IPs empty", path: "/node/handler/drop-ips", body: `{"ips":[]}`},
		{name: "drop IPs null item", path: "/node/handler/drop-ips", body: `{"ips":[null]}`},
		{name: "trailing JSON", path: "/node/handler/drop-ips", body: `{"ips":["x"]}{"ips":["y"]}`},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var providerCalls atomic.Int64
			var dropperCalls atomic.Int64
			server := &Server{handlerService: nodehandler.NewService(
				countingHandlerProvider{calls: &providerCalls},
				countingDropper{calls: &dropperCalls},
			), bodyBudget: newHTTPTestBudget(t, false, 0)}
			req := newJSONRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			rec := httptest.NewRecorder()

			server.handleNodeRoutes(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if providerCalls.Load() != 0 || dropperCalls.Load() != 0 {
				t.Fatalf("side effects provider=%d dropper=%d, want zero", providerCalls.Load(), dropperCalls.Load())
			}
		})
	}
}

func TestHandlerMutationRouteExcludesXrayStart(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	provider := &blockingHandlerProvider{
		countingHandlerProvider: countingHandlerProvider{calls: &atomic.Int64{}},
		entered:                 entered,
		release:                 release,
	}
	manager := &recordingXrayController{}
	server := &Server{
		manager:        manager,
		handlerService: nodehandler.NewService(provider, nil),
		bodyBudget:     newHTTPTestBudget(t, false, 0),
	}
	handlerRoute, _ := contractspec.FindRouteByPath("/node/handler/add-user")
	handlerResult := serveNodeRouteAsync(server, newJSONRequest(
		handlerRoute.Method,
		handlerRoute.Path,
		bytes.NewReader(handlerRoute.ValidRequest),
	))
	awaitTestSignal(t, entered, "handler mutation")

	startRoute, _ := contractspec.FindRouteByPath("/node/xray/start")
	startWaitContext, cancelStartWait := context.WithCancel(context.Background())
	defer cancelStartWait()
	startWaiting := make(chan struct{})
	startRequest := newJSONRequest(
		startRoute.Method,
		startRoute.Path,
		bytes.NewReader(startRoute.ValidRequest),
	).WithContext(&observedDoneContext{Context: startWaitContext, observed: startWaiting})
	startResult := serveNodeRouteAsync(server, startRequest)
	awaitTestSignal(t, startWaiting, "Xray start lifecycle wait")
	if manager.startCalls.Load() != 0 {
		t.Fatal("Xray start entered while a handler mutation held the lifecycle gate")
	}

	close(release)
	for name, result := range map[string]<-chan asyncRouteResult{
		"handler mutation": handlerResult,
		"Xray start":       startResult,
	} {
		outcome := awaitRouteResult(t, result, name)
		if outcome.panicValue != nil || outcome.response.Code != http.StatusOK {
			t.Fatalf("%s result: panic=%v status=%d body=%s", name, outcome.panicValue, outcome.response.Code, outcome.response.Body.String())
		}
	}
	if manager.startCalls.Load() != 1 {
		t.Fatalf("Xray start calls = %d, want 1 after mutation", manager.startCalls.Load())
	}
}

func TestHandlerRoutesProduceOfficialResponseShapes(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/node/handler/add-user",
		"/node/handler/remove-user",
		"/node/handler/get-inbound-users-count",
		"/node/handler/get-inbound-users",
		"/node/handler/add-users",
		"/node/handler/remove-users",
		"/node/handler/drop-users-connections",
		"/node/handler/drop-ips",
	}

	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			route, ok := contractspec.FindRouteByPath(path)
			if !ok {
				t.Fatalf("contract route %s is missing", path)
			}
			var providerCalls atomic.Int64
			var dropperCalls atomic.Int64
			server := &Server{handlerService: nodehandler.NewService(
				countingHandlerProvider{calls: &providerCalls},
				countingDropper{calls: &dropperCalls},
			), bodyBudget: newHTTPTestBudget(t, false, 0)}
			req := newJSONRequest(route.Method, route.Path, bytes.NewReader(route.ValidRequest))
			rec := httptest.NewRecorder()

			server.handleNodeRoutes(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			if err := contractspec.ValidateResponse(path, rec.Body.Bytes()); err != nil {
				t.Fatalf("response violates official schema: %v\n%s", err, rec.Body.Bytes())
			}
		})
	}
}

type failingInboundUsersProvider struct {
	countingHandlerProvider
}

func (p failingInboundUsersProvider) HandlerGetInboundUsersCount(context.Context, string) (int64, xrayrpc.HandlerResult) {
	p.hit()
	return 0, xrayrpc.HandlerResult{OK: false, Message: "raw SDK detail"}
}

func TestHandlerApplicationErrorUsesOfficialCodeAndPath(t *testing.T) {
	t.Parallel()

	var providerCalls atomic.Int64
	var dropperCalls atomic.Int64
	server := &Server{handlerService: nodehandler.NewService(
		failingInboundUsersProvider{countingHandlerProvider{calls: &providerCalls}},
		countingDropper{calls: &dropperCalls},
	), bodyBudget: newHTTPTestBudget(t, false, 0)}
	req := newJSONRequest(
		http.MethodPost,
		"/node/handler/get-inbound-users-count",
		strings.NewReader(`{"tag":"inbound-1"}`),
	)
	rec := httptest.NewRecorder()

	server.handleNodeRoutes(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if err := contractspec.OfficialErrors.ApplicationResponse.ValidateJSON(rec.Body.Bytes()); err != nil {
		t.Fatalf("application error violates official schema: %v\n%s", err, rec.Body.Bytes())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"errorCode":"A014"`)) ||
		!bytes.Contains(rec.Body.Bytes(), []byte(`"path":"/node/handler/get-inbound-users-count"`)) {
		t.Fatalf("unexpected application error: %s", rec.Body.Bytes())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("raw SDK detail")) {
		t.Fatalf("SDK detail leaked into official application error: %s", rec.Body.Bytes())
	}
}
