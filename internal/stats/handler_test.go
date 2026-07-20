package stats_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/luxiaba/remnanode-lite/internal/nodeapi"
	"github.com/luxiaba/remnanode-lite/internal/stats"
	"github.com/luxiaba/remnanode-lite/internal/system"
	"github.com/luxiaba/remnanode-lite/internal/xrayrpc"
)

type combinedLeaseContextKey struct{}

type combinedLeaseProvider struct {
	*mockProvider
	beginCalls   atomic.Int32
	releaseCalls atomic.Int32
	rpcCalls     atomic.Int32
}

type fixedSystemStats struct {
	value system.Stats
}

func (s fixedSystemStats) Stats() system.Stats { return s.value }

func (p *combinedLeaseProvider) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	p.beginCalls.Add(1)
	return context.WithValue(ctx, combinedLeaseContextKey{}, p), func() { p.releaseCalls.Add(1) }, nil
}

func (p *combinedLeaseProvider) GetAllInboundsStats(ctx context.Context, _ bool) ([]xrayrpc.TagTraffic, error) {
	if ctx.Value(combinedLeaseContextKey{}) != p {
		return nil, errors.New("missing combined lease context")
	}
	p.rpcCalls.Add(1)
	return []xrayrpc.TagTraffic{}, nil
}

func (p *combinedLeaseProvider) GetAllOutboundsStats(ctx context.Context, _ bool) ([]xrayrpc.TagTraffic, error) {
	if ctx.Value(combinedLeaseContextKey{}) != p {
		return nil, errors.New("missing combined lease context")
	}
	p.rpcCalls.Add(1)
	return []xrayrpc.TagTraffic{}, nil
}

type mockProvider struct {
	usersStats       []xrayrpc.UserTraffic
	usersIPList      []xrayrpc.UserIPEntry
	usersErr         error
	onlineCalls      int
	userIPListCalls  int
	onlineUsername   string
	userIPListUserID string
}

func (m *mockProvider) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	return ctx, func() {}, m.usersErr
}

func (m *mockProvider) GetSysStats(context.Context) (*xrayrpc.SysStats, error) {
	return &xrayrpc.SysStats{Uptime: 1}, m.usersErr
}
func (m *mockProvider) GetAllUsersStats(context.Context, bool) ([]xrayrpc.UserTraffic, error) {
	return m.usersStats, m.usersErr
}
func (m *mockProvider) GetUserOnlineStatus(_ context.Context, username string) (bool, error) {
	m.onlineCalls++
	m.onlineUsername = username
	return false, m.usersErr
}
func (m *mockProvider) GetInboundStats(context.Context, string, bool) (xrayrpc.TagTraffic, error) {
	return xrayrpc.TagTraffic{}, m.usersErr
}
func (m *mockProvider) GetOutboundStats(context.Context, string, bool) (xrayrpc.TagTraffic, error) {
	return xrayrpc.TagTraffic{}, m.usersErr
}
func (m *mockProvider) GetAllInboundsStats(context.Context, bool) ([]xrayrpc.TagTraffic, error) {
	return nil, m.usersErr
}
func (m *mockProvider) GetAllOutboundsStats(context.Context, bool) ([]xrayrpc.TagTraffic, error) {
	return nil, m.usersErr
}
func (m *mockProvider) GetUserIPList(_ context.Context, userID string, _ bool) ([]xrayrpc.IPEntry, error) {
	m.userIPListCalls++
	m.userIPListUserID = userID
	return nil, m.usersErr
}
func (m *mockProvider) GetUsersIPList(context.Context) ([]xrayrpc.UserIPEntry, error) {
	return m.usersIPList, m.usersErr
}

func TestGetSystemStatsReturnsErrorWhenOffline(t *testing.T) {
	t.Parallel()

	service := stats.NewService(&mockProvider{usersErr: errors.New("xray is not online")}, nil, system.NewCollector(nil))
	_, err := service.GetSystemStats(context.Background())
	assertServiceError(t, err, "A010")
}

func TestGetSystemStatsUsesInjectedSystemCollector(t *testing.T) {
	t.Parallel()
	want := system.Stats{MemoryFree: 123, MemoryUsed: 456, Uptime: 789}
	service := stats.NewService(&mockProvider{}, nil, fixedSystemStats{value: want})
	response, err := service.GetSystemStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := response.System.Stats
	if got.MemoryFree != want.MemoryFree || got.MemoryUsed != want.MemoryUsed || got.Uptime != want.Uptime {
		t.Fatalf("system stats = %+v, want %+v", got, want)
	}
}

func TestGetUsersStatsFiltersZeroTraffic(t *testing.T) {
	t.Parallel()

	service := stats.NewService(&mockProvider{usersStats: []xrayrpc.UserTraffic{
		{Username: "idle", Uplink: 0, Downlink: 0},
		{Username: "active", Uplink: 10, Downlink: 5},
	}}, nil, system.NewCollector(nil))

	response, err := service.GetUsersStats(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Users) != 1 || response.Users[0].Username != "active" {
		t.Fatalf("users = %+v, want only active user", response.Users)
	}
}

func TestGetUsersStatsGRPCError(t *testing.T) {
	t.Parallel()

	service := stats.NewService(&mockProvider{usersErr: errors.New("grpc down")}, nil, system.NewCollector(nil))
	_, err := service.GetUsersStats(context.Background(), false)
	assertServiceError(t, err, "A011")
}

func TestSingleTagStatsErrorsUseOfficialCodes(t *testing.T) {
	t.Parallel()

	service := stats.NewService(&mockProvider{usersErr: errors.New("stats not found")}, nil, system.NewCollector(nil))
	_, inboundErr := service.GetInboundStats(context.Background(), "missing", false)
	assertServiceError(t, inboundErr, "A012")
	_, outboundErr := service.GetOutboundStats(context.Background(), "missing", false)
	assertServiceError(t, outboundErr, "A013")
}

func TestGetUserOnlineStatusGRPCError(t *testing.T) {
	t.Parallel()

	service := stats.NewService(&mockProvider{usersErr: errors.New("grpc down")}, nil, system.NewCollector(nil))
	response := service.GetUserOnlineStatus(context.Background(), "u1")
	if response.IsOnline {
		t.Fatal("expected isOnline=false when provider returns error")
	}
}

func TestEmptyStringRequestsReachProvider(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{}
	service := stats.NewService(provider, nil, system.NewCollector(nil))
	service.GetUserOnlineStatus(context.Background(), "")
	service.GetUserIPList(context.Background(), "")

	if provider.onlineCalls != 1 || provider.userIPListCalls != 1 {
		t.Fatalf("provider calls online=%d userIPList=%d, want one each", provider.onlineCalls, provider.userIPListCalls)
	}
	if provider.onlineUsername != "" || provider.userIPListUserID != "" {
		t.Fatalf("provider received username=%q userID=%q, want empty strings", provider.onlineUsername, provider.userIPListUserID)
	}
}

func TestGetUserIPListGRPCError(t *testing.T) {
	t.Parallel()

	service := stats.NewService(&mockProvider{usersErr: errors.New("grpc down")}, nil, system.NewCollector(nil))
	response := service.GetUserIPList(context.Background(), "u1")
	if len(response.IPs) != 0 {
		t.Fatalf("ips = %+v, want empty", response.IPs)
	}
}

func TestGetUsersIPListGRPCError(t *testing.T) {
	t.Parallel()

	service := stats.NewService(&mockProvider{usersErr: errors.New("grpc down")}, nil, system.NewCollector(nil))
	response := service.GetUsersIPList(context.Background())
	if len(response.Users) != 0 {
		t.Fatalf("users = %+v, want empty", response.Users)
	}
}

func TestGetUsersIPListPreservesNativeUsersWithEmptyIPs(t *testing.T) {
	t.Parallel()

	service := stats.NewService(&mockProvider{usersIPList: []xrayrpc.UserIPEntry{
		{UserID: "u1", IPs: []xrayrpc.IPEntry{}},
	}}, nil, system.NewCollector(nil))
	response := service.GetUsersIPList(context.Background())
	if len(response.Users) != 1 || response.Users[0].UserID != "u1" || len(response.Users[0].IPs) != 0 {
		t.Fatalf("users = %+v, want native empty-IP entry preserved", response.Users)
	}
}

func TestGetCombinedStatsUsesOneLeaseForBothRPCs(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprintf("reset=%v", reset), func(t *testing.T) {
			provider := &combinedLeaseProvider{mockProvider: &mockProvider{}}
			service := stats.NewService(provider, nil, system.NewCollector(nil))
			if _, err := service.GetCombinedStats(context.Background(), reset); err != nil {
				t.Fatal(err)
			}
			if provider.beginCalls.Load() != 1 || provider.releaseCalls.Load() != 1 || provider.rpcCalls.Load() != 2 {
				t.Fatalf("lease/RPC calls = %d/%d/%d, want 1/1/2",
					provider.beginCalls.Load(), provider.releaseCalls.Load(), provider.rpcCalls.Load())
			}
		})
	}
}

func assertServiceError(t *testing.T, err error, code string) {
	t.Helper()
	serviceError, ok := nodeapi.AsServiceError(err)
	if !ok {
		t.Fatalf("error = %v, want ServiceError", err)
	}
	if serviceError.Code != code {
		t.Fatalf("error code = %q, want %q", serviceError.Code, code)
	}
}
