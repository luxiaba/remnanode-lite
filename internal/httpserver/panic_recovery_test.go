package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	contractspec "github.com/Luxiaba/remnanode-lite/internal/contract"
	"github.com/Luxiaba/remnanode-lite/internal/nodeapi"
	"github.com/Luxiaba/remnanode-lite/internal/nodehandler"
	"github.com/Luxiaba/remnanode-lite/internal/xtls"
)

type routePanicProvider struct {
	panicNext atomic.Bool
	active    atomic.Bool
	releases  atomic.Int32
}

func (p *routePanicProvider) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	if !p.active.CompareAndSwap(false, true) {
		panic("overlapping test mutation lease")
	}
	return ctx, func() {
		if !p.active.CompareAndSwap(true, false) {
			panic("released inactive test mutation lease")
		}
		p.releases.Add(1)
	}, nil
}

func (p *routePanicProvider) InboundTags() []string {
	if !p.active.Load() {
		panic("InboundTags called outside mutation lease")
	}
	if p.panicNext.Swap(false) {
		panic("sensitive-route-panic")
	}
	return []string{"in-1"}
}

func (*routePanicProvider) GetUserIPList(context.Context, string, bool) ([]xtls.IPEntry, error) {
	return nil, nil
}

func (*routePanicProvider) HandlerRemoveUser(context.Context, string, string, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*routePanicProvider) HandlerAddVlessUser(context.Context, string, string, string, string, uint32, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*routePanicProvider) HandlerAddTrojanUser(context.Context, string, string, string, uint32, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*routePanicProvider) HandlerAddShadowsocksUser(context.Context, string, string, string, int, bool, uint32, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*routePanicProvider) HandlerAddShadowsocks2022User(context.Context, string, string, string, uint32, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*routePanicProvider) HandlerAddHysteriaUser(context.Context, string, string, string, uint32, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*routePanicProvider) HandlerGetInboundUsers(context.Context, string) ([]xtls.InboundUser, xtls.HandlerResult) {
	return nil, xtls.HandlerResult{OK: true}
}

func (*routePanicProvider) HandlerGetInboundUsersCount(context.Context, string) (int64, xtls.HandlerResult) {
	return 0, xtls.HandlerResult{OK: true}
}

func TestMutationPanicThroughHTTPReturnsOfficialErrorAndRecovers(t *testing.T) {
	const panicValue = "sensitive-route-panic"
	route, ok := contractspec.FindRouteByPath("/node/handler/add-user")
	if !ok {
		t.Fatal("add-user contract route is missing")
	}
	provider := &routePanicProvider{}
	provider.panicNext.Store(true)
	server := &Server{
		handlerService: nodehandler.NewService(provider, nil),
		bodyBudget:     newHTTPTestBudget(t, false, 0),
	}
	handler := server.nodeRequestHandler(defaultMaxHandlers)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, newJSONRequest(route.Method, route.Path, strings.NewReader(string(route.ValidRequest))))
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("panic response status = %d, want 500; body=%s", first.Code, first.Body.String())
	}
	if strings.Contains(first.Body.String(), panicValue) {
		t.Fatalf("panic response leaked diagnostic: %s", first.Body.String())
	}
	var applicationError nodeapi.ApplicationError
	if err := json.Unmarshal(first.Body.Bytes(), &applicationError); err != nil {
		t.Fatal(err)
	}
	if applicationError.ErrorCode != "A001" || applicationError.Message != "Server error" || applicationError.Path != route.Path {
		t.Fatalf("panic response = %+v, want official A001", applicationError)
	}
	if err := contractspec.OfficialErrors.ApplicationResponse.ValidateJSON(first.Body.Bytes()); err != nil {
		t.Fatalf("panic response violates official schema: %v\n%s", err, first.Body.Bytes())
	}
	if provider.active.Load() || provider.releases.Load() != 1 {
		t.Fatalf("lease after panic: active=%v releases=%d", provider.active.Load(), provider.releases.Load())
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, newJSONRequest(route.Method, route.Path, strings.NewReader(string(route.ValidRequest))))
	if second.Code != http.StatusOK {
		t.Fatalf("request after panic status = %d, want 200; body=%s", second.Code, second.Body.String())
	}
	if err := contractspec.ValidateResponse(route.Path, second.Body.Bytes()); err != nil {
		t.Fatalf("response after panic violates official schema: %v\n%s", err, second.Body.Bytes())
	}
	if provider.active.Load() || provider.releases.Load() != 2 {
		t.Fatalf("lease after recovery: active=%v releases=%d", provider.active.Load(), provider.releases.Load())
	}
}
