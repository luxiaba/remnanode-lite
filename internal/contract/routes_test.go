package contract_test

import (
	"reflect"
	"sort"
	"testing"

	contractspec "github.com/luxiaba/remnanode-lite/internal/contract"
	"github.com/luxiaba/remnanode-lite/internal/httpserver"
)

// The evidence package is independent from the dispatcher registry. Comparing
// them prevents a hand-maintained "implemented" map from self-passing.
var officialRoutes = func() []httpserver.NodeRoute {
	contracts := contractspec.OfficialRoutes()
	routes := make([]httpserver.NodeRoute, 0, len(contracts))
	for _, route := range contracts {
		routes = append(routes, httpserver.NodeRoute{Method: route.Method, Path: route.Path})
	}
	return routes
}()

func TestOfficialRouteRegistry(t *testing.T) {
	t.Parallel()

	want := append([]httpserver.NodeRoute(nil), officialRoutes...)
	sortRoutes(want)
	got := httpserver.RegisteredNodeRoutes()

	if len(got) != 25 {
		t.Fatalf("registered route count = %d, want 25", len(got))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registered routes do not match official 3.4.1\n got: %#v\nwant: %#v", got, want)
	}
}

func TestRemovedHandlerQueryRoutesAreNotExposed(t *testing.T) {
	t.Parallel()

	registered := httpserver.RegisteredNodeRoutes()
	for _, removed := range []string{
		"/node/handler/get-inbound-users",
		"/node/handler/get-inbound-users-count",
	} {
		if _, ok := contractspec.FindRouteByPath(removed); ok {
			t.Errorf("removed route remains in contract: %s", removed)
		}
		for _, route := range registered {
			if route.Path == removed {
				t.Errorf("removed route remains registered: %s", removed)
			}
		}
	}
}

func sortRoutes(routes []httpserver.NodeRoute) {
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path == routes[j].Path {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Path < routes[j].Path
	})
}
