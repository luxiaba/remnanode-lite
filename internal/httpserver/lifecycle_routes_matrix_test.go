package httpserver

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	contractspec "github.com/luxiaba/remnanode-lite/internal/contract"
	"github.com/luxiaba/remnanode-lite/internal/nodehandler"
	"github.com/luxiaba/remnanode-lite/internal/stats"
	"github.com/luxiaba/remnanode-lite/internal/xrayrpc"
)

type lifecycleRouteCase struct {
	name      string
	path      string
	body      string
	newServer func(*testing.T, *atomic.Int64, *atomic.Bool, chan struct{}, <-chan struct{}) *Server
}

type lifecycleStatsProvider struct {
	calls       *atomic.Int64
	sawNonReset *atomic.Bool
	entered     chan struct{}
	release     <-chan struct{}
	enteredOnce sync.Once
}

func (p *lifecycleStatsProvider) wait(ctx context.Context) error {
	p.calls.Add(1)
	p.enteredOnce.Do(func() { close(p.entered) })
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *lifecycleStatsProvider) waitForReset(ctx context.Context, reset bool) error {
	if !reset {
		p.sawNonReset.Store(true)
	}
	return p.wait(ctx)
}

func (p *lifecycleStatsProvider) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	if err := p.wait(ctx); err != nil {
		return nil, nil, err
	}
	return ctx, func() {}, nil
}

func (p *lifecycleStatsProvider) GetSysStats(ctx context.Context) (*xrayrpc.SysStats, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return &xrayrpc.SysStats{}, nil
}

func (p *lifecycleStatsProvider) GetAllUsersStats(ctx context.Context, reset bool) ([]xrayrpc.UserTraffic, error) {
	if err := p.waitForReset(ctx, reset); err != nil {
		return nil, err
	}
	return []xrayrpc.UserTraffic{}, nil
}

func (p *lifecycleStatsProvider) GetUserOnlineStatus(ctx context.Context, _ string) (bool, error) {
	return false, p.wait(ctx)
}

func (p *lifecycleStatsProvider) GetInboundStats(ctx context.Context, _ string, reset bool) (xrayrpc.TagTraffic, error) {
	if err := p.waitForReset(ctx, reset); err != nil {
		return xrayrpc.TagTraffic{}, err
	}
	return xrayrpc.TagTraffic{Tag: "inbound-1"}, nil
}

func (p *lifecycleStatsProvider) GetOutboundStats(ctx context.Context, _ string, reset bool) (xrayrpc.TagTraffic, error) {
	if err := p.waitForReset(ctx, reset); err != nil {
		return xrayrpc.TagTraffic{}, err
	}
	return xrayrpc.TagTraffic{Tag: "outbound-1"}, nil
}

func (p *lifecycleStatsProvider) GetAllInboundsStats(ctx context.Context, reset bool) ([]xrayrpc.TagTraffic, error) {
	if err := p.waitForReset(ctx, reset); err != nil {
		return nil, err
	}
	return []xrayrpc.TagTraffic{}, nil
}

func (p *lifecycleStatsProvider) GetAllOutboundsStats(ctx context.Context, reset bool) ([]xrayrpc.TagTraffic, error) {
	if err := p.waitForReset(ctx, reset); err != nil {
		return nil, err
	}
	return []xrayrpc.TagTraffic{}, nil
}

func (p *lifecycleStatsProvider) GetUserIPList(ctx context.Context, _ string, reset bool) ([]xrayrpc.IPEntry, error) {
	if err := p.waitForReset(ctx, reset); err != nil {
		return nil, err
	}
	return []xrayrpc.IPEntry{}, nil
}

func (p *lifecycleStatsProvider) GetUsersIPList(ctx context.Context) ([]xrayrpc.UserIPEntry, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return []xrayrpc.UserIPEntry{}, nil
}

type lifecycleHandlerProvider struct {
	countingHandlerProvider
	entered     chan struct{}
	release     <-chan struct{}
	enteredOnce sync.Once
}

func (p *lifecycleHandlerProvider) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	p.hit()
	p.enteredOnce.Do(func() { close(p.entered) })
	select {
	case <-p.release:
		return ctx, func() {}, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func lifecycleMutationRouteCases() []lifecycleRouteCase {
	newHandlerServer := func(
		t *testing.T,
		calls *atomic.Int64,
		_ *atomic.Bool,
		entered chan struct{},
		release <-chan struct{},
	) *Server {
		t.Helper()
		provider := &lifecycleHandlerProvider{
			countingHandlerProvider: countingHandlerProvider{calls: calls},
			entered:                 entered,
			release:                 release,
		}
		return &Server{
			handlerService: nodehandler.NewService(provider, nil),
			bodyBudget:     newHTTPTestBudget(t, false, 0),
		}
	}
	newStatsServer := func(
		t *testing.T,
		calls *atomic.Int64,
		sawNonReset *atomic.Bool,
		entered chan struct{},
		release <-chan struct{},
	) *Server {
		t.Helper()
		provider := &lifecycleStatsProvider{
			calls:       calls,
			sawNonReset: sawNonReset,
			entered:     entered,
			release:     release,
		}
		return &Server{
			statsService: stats.NewService(provider, nil, nil),
			bodyBudget:   newHTTPTestBudget(t, false, 0),
		}
	}

	return []lifecycleRouteCase{
		{name: "handler add user", path: "/node/handler/add-user", newServer: newHandlerServer},
		{name: "handler remove user", path: "/node/handler/remove-user", newServer: newHandlerServer},
		{name: "handler add users", path: "/node/handler/add-users", newServer: newHandlerServer},
		{name: "handler remove users", path: "/node/handler/remove-users", newServer: newHandlerServer},
		{name: "stats users reset", path: "/node/stats/get-users-stats", body: `{"reset":true}`, newServer: newStatsServer},
		{name: "stats inbound reset", path: "/node/stats/get-inbound-stats", body: `{"tag":"inbound-1","reset":true}`, newServer: newStatsServer},
		{name: "stats outbound reset", path: "/node/stats/get-outbound-stats", body: `{"tag":"outbound-1","reset":true}`, newServer: newStatsServer},
		{name: "stats all inbounds reset", path: "/node/stats/get-all-inbounds-stats", body: `{"reset":true}`, newServer: newStatsServer},
		{name: "stats all outbounds reset", path: "/node/stats/get-all-outbounds-stats", body: `{"reset":true}`, newServer: newStatsServer},
		{name: "stats combined reset", path: "/node/stats/get-combined-stats", body: `{"reset":true}`, newServer: newStatsServer},
		{name: "stats user IP list resets", path: "/node/stats/get-user-ip-list", newServer: newStatsServer},
	}
}

func TestExclusiveMutationRouteMatrixBlocksXrayStartAndStop(t *testing.T) {
	for _, test := range lifecycleMutationRouteCases() {
		t.Run(test.name, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			var providerCalls atomic.Int64
			var sawNonReset atomic.Bool
			manager := &recordingXrayController{}
			server := test.newServer(t, &providerCalls, &sawNonReset, entered, release)
			server.manager = manager
			server.pluginService = &recordingPluginController{}

			mutationResult := serveNodeRouteAsync(server, lifecycleRouteRequest(t, test))
			awaitTestSignal(t, entered, test.name+" provider")
			assertLifecycleGateHeld(t, server, test.name)

			startCancel, startResult := startCanceledLifecycleWait(t, server, "/node/xray/start")
			stopCancel, stopResult := startCanceledLifecycleWait(t, server, "/node/xray/stop")
			if manager.startCalls.Load() != 0 || manager.stopCalls.Load() != 0 {
				t.Fatalf("Xray controllers entered behind %s: start=%d stop=%d", test.name, manager.startCalls.Load(), manager.stopCalls.Load())
			}

			startCancel()
			stopCancel()
			assertCanceledRouteWait(t, startResult, "Xray start")
			assertCanceledRouteWait(t, stopResult, "Xray stop")
			if manager.startCalls.Load() != 0 || manager.stopCalls.Load() != 0 {
				t.Fatalf("canceled Xray controllers were called behind %s: start=%d stop=%d", test.name, manager.startCalls.Load(), manager.stopCalls.Load())
			}

			releaseOnce.Do(func() { close(release) })
			outcome := awaitRouteResult(t, mutationResult, test.name)
			if outcome.panicValue != nil || outcome.response.Code != http.StatusOK {
				t.Fatalf("%s result: panic=%v status=%d body=%s", test.name, outcome.panicValue, outcome.response.Code, outcome.response.Body.String())
			}
			if sawNonReset.Load() {
				t.Fatalf("%s reached its stats provider without reset=true", test.name)
			}
		})
	}
}

func TestExclusiveMutationRouteMatrixCancellationSkipsProvider(t *testing.T) {
	for _, test := range lifecycleMutationRouteCases() {
		t.Run(test.name, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			var providerCalls atomic.Int64
			var sawNonReset atomic.Bool
			server := test.newServer(t, &providerCalls, &sawNonReset, entered, release)
			if !server.acquireXrayLifecycle(context.Background()) {
				t.Fatal("failed to occupy Xray lifecycle gate")
			}
			t.Cleanup(server.releaseXrayLifecycle)

			waitContext, cancelWait := context.WithCancel(context.Background())
			observed := make(chan struct{})
			request := lifecycleRouteRequest(t, test).WithContext(
				&observedDoneContext{Context: waitContext, observed: observed},
			)
			result := serveNodeRouteAsync(server, request)
			awaitTestSignal(t, observed, test.name+" lifecycle wait")
			if calls := providerCalls.Load(); calls != 0 {
				t.Fatalf("provider calls while %s waited for lifecycle gate = %d, want 0", test.name, calls)
			}

			cancelWait()
			assertCanceledRouteWait(t, result, test.name)
			if calls := providerCalls.Load(); calls != 0 {
				t.Fatalf("provider calls after canceling %s lifecycle wait = %d, want 0", test.name, calls)
			}
		})
	}
}

func lifecycleRouteRequest(t *testing.T, test lifecycleRouteCase) *http.Request {
	t.Helper()
	route, ok := contractspec.FindRouteByPath(test.path)
	if !ok {
		t.Fatalf("contract route %s is missing", test.path)
	}
	body := test.body
	if body == "" {
		body = string(route.ValidRequest)
	}
	return newJSONRequest(route.Method, route.Path, bytes.NewBufferString(body))
}

func startCanceledLifecycleWait(
	t *testing.T,
	server *Server,
	path string,
) (context.CancelFunc, <-chan asyncRouteResult) {
	t.Helper()
	route, ok := contractspec.FindRouteByPath(path)
	if !ok {
		t.Fatalf("contract route %s is missing", path)
	}
	waitContext, cancelWait := context.WithCancel(context.Background())
	observed := make(chan struct{})
	request := newJSONRequest(route.Method, route.Path, bytes.NewReader(route.ValidRequest)).WithContext(
		&observedDoneContext{Context: waitContext, observed: observed},
	)
	result := serveNodeRouteAsync(server, request)
	awaitTestSignal(t, observed, path+" lifecycle wait")
	return cancelWait, result
}

func assertCanceledRouteWait(t *testing.T, result <-chan asyncRouteResult, name string) {
	t.Helper()
	outcome := awaitRouteResult(t, result, name+" cancellation")
	panicErr, ok := outcome.panicValue.(error)
	if !ok || !errors.Is(panicErr, http.ErrAbortHandler) {
		t.Fatalf("%s cancellation panic = %#v, want http.ErrAbortHandler", name, outcome.panicValue)
	}
	if outcome.response.Body.Len() != 0 {
		t.Fatalf("%s cancellation wrote response body %q", name, outcome.response.Body.String())
	}
}
