package nodehandler_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/luxiaba/remnanode-lite/internal/connections"
	"github.com/luxiaba/remnanode-lite/internal/nodehandler"
	"github.com/luxiaba/remnanode-lite/internal/xrayrpc"
)

type mutationLeaseContextKey struct{}

type mutationLeaseRecorder struct {
	mu sync.Mutex

	active     bool
	marker     *struct{}
	beginCalls int
	releases   int
	events     []string
	violations []string
}

func (r *mutationLeaseRecorder) begin(ctx context.Context) (context.Context, func(), error) {
	r.mu.Lock()
	r.beginCalls++
	r.events = append(r.events, "begin")
	if r.active {
		r.violations = append(r.violations, "BeginMutation called while a lease was active")
	}
	r.active = true
	r.marker = &struct{}{}
	marker := r.marker
	r.mu.Unlock()

	leaseCtx := context.WithValue(ctx, mutationLeaseContextKey{}, marker)
	return leaseCtx, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.releases++
		r.events = append(r.events, "release")
		if !r.active {
			r.violations = append(r.violations, "lease released while inactive")
		}
		r.active = false
	}, nil
}

func (r *mutationLeaseRecorder) record(name string, ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, name)
	if !r.active {
		r.violations = append(r.violations, name+" called outside the active lease")
	}
	if ctx != nil && ctx.Value(mutationLeaseContextKey{}) != r.marker {
		r.violations = append(r.violations, name+" called without the lease context")
	}
}

func (r *mutationLeaseRecorder) snapshot() (int, int, bool, []string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.beginCalls,
		r.releases,
		r.active,
		append([]string(nil), r.events...),
		append([]string(nil), r.violations...)
}

type recordingMutationProvider struct {
	recorder *mutationLeaseRecorder
}

func (p *recordingMutationProvider) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	return p.recorder.begin(ctx)
}

func (p *recordingMutationProvider) InboundTags() []string {
	p.recorder.record("inbound_tags", nil)
	return []string{"in-1"}
}

func (p *recordingMutationProvider) GetUserIPList(ctx context.Context, userID string, reset bool) ([]xrayrpc.IPEntry, error) {
	p.recorder.record(fmt.Sprintf("get_user_ip_list:%s:%t", userID, reset), ctx)
	return []xrayrpc.IPEntry{{IP: "203.0.113.10"}}, nil
}

func (p *recordingMutationProvider) HandlerRemoveUser(ctx context.Context, tag, username, _ string) xrayrpc.HandlerResult {
	p.recorder.record("handler_remove:"+tag+":"+username, ctx)
	return xrayrpc.HandlerResult{OK: true}
}

func (p *recordingMutationProvider) HandlerAddVlessUser(ctx context.Context, tag, username, _, _ string, _ uint32, _ string) xrayrpc.HandlerResult {
	p.recorder.record("handler_add_vless:"+tag+":"+username, ctx)
	return xrayrpc.HandlerResult{OK: true}
}

func (p *recordingMutationProvider) HandlerAddTrojanUser(ctx context.Context, tag, username, _ string, _ uint32, _ string) xrayrpc.HandlerResult {
	p.recorder.record("handler_add_trojan:"+tag+":"+username, ctx)
	return xrayrpc.HandlerResult{OK: true}
}

func (p *recordingMutationProvider) HandlerAddShadowsocksUser(ctx context.Context, tag, username, _ string, _ int, _ bool, _ uint32, _ string) xrayrpc.HandlerResult {
	p.recorder.record("handler_add_shadowsocks:"+tag+":"+username, ctx)
	return xrayrpc.HandlerResult{OK: true}
}

func (p *recordingMutationProvider) HandlerAddShadowsocks2022User(ctx context.Context, tag, username, _ string, _ uint32, _ string) xrayrpc.HandlerResult {
	p.recorder.record("handler_add_shadowsocks2022:"+tag+":"+username, ctx)
	return xrayrpc.HandlerResult{OK: true}
}

func (p *recordingMutationProvider) HandlerAddHysteriaUser(ctx context.Context, tag, username, _ string, _ uint32, _ string) xrayrpc.HandlerResult {
	p.recorder.record("handler_add_hysteria:"+tag+":"+username, ctx)
	return xrayrpc.HandlerResult{OK: true}
}

func (p *recordingMutationProvider) HandlerGetInboundUsers(ctx context.Context, tag string) ([]xrayrpc.InboundUser, xrayrpc.HandlerResult) {
	p.recorder.record("handler_get_inbound_users:"+tag, ctx)
	return nil, xrayrpc.HandlerResult{OK: true}
}

func (p *recordingMutationProvider) HandlerGetInboundUsersCount(ctx context.Context, tag string) (int64, xrayrpc.HandlerResult) {
	p.recorder.record("handler_get_inbound_users_count:"+tag, ctx)
	return 0, xrayrpc.HandlerResult{OK: true}
}

type recordingMutationDropper struct {
	recorder *mutationLeaseRecorder
}

func (d *recordingMutationDropper) Available() bool {
	d.recorder.record("drop_available", nil)
	return true
}

func (d *recordingMutationDropper) DropIPs(ctx context.Context, ips []string) bool {
	d.recorder.record("drop_ips:"+fmt.Sprint(ips), ctx)
	return true
}

func (*recordingMutationDropper) DropUsers(context.Context, connections.IPListProvider, []string) bool {
	panic("unexpected DropUsers call")
}

func TestTopLevelMutationsHoldOneCoreLeaseAcrossAllSideEffects(t *testing.T) {
	t.Parallel()

	addUserRequest := nodehandler.AddUserRequest{
		Data: []nodehandler.AddUserItem{
			{Type: "vless", Tag: "in-1", Username: "u1", UUID: "uuid-1"},
			{Type: "trojan", Tag: "in-1", Username: "u1", Password: "trojan-secret"},
			{Type: "shadowsocks", Tag: "in-1", Username: "u1", Password: "ss-secret"},
			{Type: "shadowsocks22", Tag: "in-1", Username: "u1", Password: "ss-2022-secret"},
			{Type: "hysteria", Tag: "in-1", Username: "u1", Password: "hysteria-secret"},
		},
		HashData: nodehandler.AddUserHashData{VlessUUID: "hash-1"},
	}
	addUsersRequest := nodehandler.AddUsersRequest{
		AffectedInboundTags: []string{"in-1"},
		Users: []nodehandler.BatchUser{{
			InboundData: []nodehandler.BatchInbound{
				{Type: "vless", Tag: "in-1"},
				{Type: "trojan", Tag: "in-1"},
				{Type: "shadowsocks", Tag: "in-1"},
				{Type: "shadowsocks22", Tag: "in-1"},
				{Type: "hysteria", Tag: "in-1"},
			},
			UserData: nodehandler.BatchUserData{
				UserID:         "u1",
				HashUUID:       "hash-1",
				VlessUUID:      "uuid-1",
				TrojanPassword: "trojan-secret",
				SSPassword:     "ss-secret",
			},
		}},
	}

	tests := []struct {
		name       string
		call       func(*nodehandler.Service) (nodehandler.GenericResponse, error)
		wantEvents []string
	}{
		{
			name: "AddUser",
			call: func(service *nodehandler.Service) (nodehandler.GenericResponse, error) {
				return service.AddUser(context.Background(), addUserRequest)
			},
			wantEvents: []string{
				"begin",
				"inbound_tags",
				"handler_remove:in-1:u1",
				"handler_add_vless:in-1:u1",
				"handler_add_trojan:in-1:u1",
				"handler_add_shadowsocks:in-1:u1",
				"handler_add_shadowsocks2022:in-1:u1",
				"handler_add_hysteria:in-1:u1",
				"release",
			},
		},
		{
			name: "RemoveUser",
			call: func(service *nodehandler.Service) (nodehandler.GenericResponse, error) {
				return service.RemoveUser(context.Background(), nodehandler.RemoveUserRequest{
					Username: "u1", VlessUUID: "hash-1",
				})
			},
			wantEvents: []string{
				"begin",
				"inbound_tags",
				"drop_available",
				"get_user_ip_list:u1:false",
				"handler_remove:in-1:u1",
				"drop_ips:[203.0.113.10]",
				"release",
			},
		},
		{
			name: "AddUsers",
			call: func(service *nodehandler.Service) (nodehandler.GenericResponse, error) {
				return service.AddUsers(context.Background(), addUsersRequest)
			},
			wantEvents: []string{
				"begin",
				"inbound_tags",
				"handler_remove:in-1:u1",
				"handler_add_vless:in-1:u1",
				"handler_add_trojan:in-1:u1",
				"handler_add_shadowsocks:in-1:u1",
				"handler_add_shadowsocks2022:in-1:u1",
				"handler_add_hysteria:in-1:u1",
				"release",
			},
		},
		{
			name: "RemoveUsers",
			call: func(service *nodehandler.Service) (nodehandler.GenericResponse, error) {
				return service.RemoveUsers(context.Background(), nodehandler.RemoveUsersRequest{
					Users: []nodehandler.RemoveUsersItem{{UserID: "u1", HashUUID: "hash-1"}},
				})
			},
			wantEvents: []string{
				"begin",
				"inbound_tags",
				"drop_available",
				"get_user_ip_list:u1:false",
				"handler_remove:in-1:u1",
				"drop_ips:[203.0.113.10]",
				"release",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			recorder := &mutationLeaseRecorder{}
			provider := &recordingMutationProvider{recorder: recorder}
			dropper := &recordingMutationDropper{recorder: recorder}
			service := nodehandler.NewService(provider, dropper)

			response, err := test.call(service)
			if err != nil {
				t.Fatalf("mutation error = %v", err)
			}
			if !response.Success || response.Error != nil {
				t.Fatalf("mutation response = %#v, want success", response)
			}

			beginCalls, releases, active, events, violations := recorder.snapshot()
			if beginCalls != 1 {
				t.Errorf("BeginMutation calls = %d, want 1", beginCalls)
			}
			if releases != 1 {
				t.Errorf("lease releases = %d, want 1", releases)
			}
			if active {
				t.Error("lease remains active after top-level mutation returned")
			}
			if len(violations) != 0 {
				t.Errorf("lease boundary violations = %v", violations)
			}
			if !slices.Equal(events, test.wantEvents) {
				t.Errorf("events = %v, want %v", events, test.wantEvents)
			}
		})
	}
}
