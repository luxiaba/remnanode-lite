package nodehandler_test

import (
	"context"
	"testing"

	"github.com/luxiaba/remnanode-lite/internal/connections"
	"github.com/luxiaba/remnanode-lite/internal/nodeapi"
	"github.com/luxiaba/remnanode-lite/internal/nodehandler"
	"github.com/luxiaba/remnanode-lite/internal/xrayrpc"
)

type stubProvider struct {
	inboundTags []string
}

func (s *stubProvider) AddInboundTag(tag string) {
	s.inboundTags = append(s.inboundTags, tag)
}
func (s *stubProvider) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	return ctx, func() {}, nil
}
func (s *stubProvider) InboundTags() []string { return s.inboundTags }
func (s *stubProvider) GetUserIPList(context.Context, string, bool) ([]xrayrpc.IPEntry, error) {
	return nil, nil
}
func (s *stubProvider) HandlerRemoveUser(context.Context, string, string, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: true}
}
func (s *stubProvider) HandlerAddVlessUser(context.Context, string, string, string, string, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: false, Message: "boom"}
}
func (s *stubProvider) HandlerAddTrojanUser(context.Context, string, string, string, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: true}
}
func (s *stubProvider) HandlerAddShadowsocksUser(context.Context, string, string, string, int, bool, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: true}
}
func (s *stubProvider) HandlerAddShadowsocks2022User(context.Context, string, string, string, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: true}
}
func (s *stubProvider) HandlerAddHysteriaUser(context.Context, string, string, string, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: true}
}
func (s *stubProvider) HandlerGetInboundUsers(context.Context, string) ([]xrayrpc.InboundUser, xrayrpc.HandlerResult) {
	return nil, xrayrpc.HandlerResult{OK: true}
}
func (s *stubProvider) HandlerGetInboundUsersCount(context.Context, string) (int64, xrayrpc.HandlerResult) {
	return 0, xrayrpc.HandlerResult{OK: true}
}

func TestAddUsersReportsHandlerFailure(t *testing.T) {
	t.Parallel()

	service := nodehandler.NewService(&stubProvider{}, connections.NewDropper(nil))
	response, err := service.AddUsers(context.Background(), nodehandler.AddUsersRequest{
		AffectedInboundTags: []string{"in-1"},
		Users: []nodehandler.BatchUser{{
			InboundData: []nodehandler.BatchInbound{{Type: "vless", Tag: "in-1", Flow: ""}},
			UserData: nodehandler.BatchUserData{
				UserID: "u1", HashUUID: "h1", VlessUUID: "uuid-1",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Success || response.Error == nil || *response.Error != "boom" {
		t.Fatalf("response = %+v, want handler failure", response)
	}
}

type failingInboundProvider struct {
	stubProvider
}

func (failingInboundProvider) HandlerGetInboundUsersCount(context.Context, string) (int64, xrayrpc.HandlerResult) {
	return 0, xrayrpc.HandlerResult{OK: false, Message: "xray is not online"}
}

func TestGetInboundUsersCountGRPCFailure(t *testing.T) {
	t.Parallel()

	service := nodehandler.NewService(&failingInboundProvider{}, connections.NewDropper(nil))
	_, err := service.GetInboundUsersCount(context.Background(), "in-1")
	serviceError, ok := nodeapi.AsServiceError(err)
	if !ok || serviceError.Code != "A014" || serviceError.Message != "Failed to get inbound users" {
		t.Fatalf("error = %+v, want A014", err)
	}
}

func TestAddUserReportsFailureWhenAllFail(t *testing.T) {
	t.Parallel()

	service := nodehandler.NewService(&stubProvider{}, connections.NewDropper(nil))
	response, err := service.AddUser(context.Background(), nodehandler.AddUserRequest{
		Data: []nodehandler.AddUserItem{{
			Type: "vless", Tag: "in-1", Username: "u1", UUID: "x", Flow: "",
		}},
		HashData: nodehandler.AddUserHashData{VlessUUID: "uuid-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Success || response.Error == nil || *response.Error != "boom" {
		t.Fatalf("response = %+v, want handler failure", response)
	}
}

func TestAddUserEmptyDataFailsWithoutPanic(t *testing.T) {
	t.Parallel()

	service := nodehandler.NewService(&stubProvider{inboundTags: []string{"in-1"}}, connections.NewDropper(nil))
	response, err := service.AddUser(context.Background(), nodehandler.AddUserRequest{
		HashData: nodehandler.AddUserHashData{VlessUUID: "uuid-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Success || response.Error != nil {
		t.Fatalf("response = %+v, want safe failure", response)
	}
}

type shadowsocksTrackingProvider struct {
	stubProvider
	ivChecks []bool
}

func (p *shadowsocksTrackingProvider) HandlerAddShadowsocksUser(_ context.Context, _, _, _ string, _ int, ivCheck bool, _ uint32, _ string) xrayrpc.HandlerResult {
	p.ivChecks = append(p.ivChecks, ivCheck)
	return xrayrpc.HandlerResult{OK: true}
}

func TestAddUserMatchesOfficialShadowsocksIVCheckBehavior(t *testing.T) {
	t.Parallel()

	provider := &shadowsocksTrackingProvider{}
	service := nodehandler.NewService(provider, connections.NewDropper(nil))
	_, err := service.AddUser(context.Background(), nodehandler.AddUserRequest{
		Data: []nodehandler.AddUserItem{{
			Type: "shadowsocks", Tag: "in-1", Username: "u1", Password: "secret", CipherType: 5, IVCheck: true,
		}},
		HashData: nodehandler.AddUserHashData{VlessUUID: "uuid-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.ivChecks) != 1 || provider.ivChecks[0] {
		t.Fatalf("ivCheck calls = %v, want [false] matching official 2.8.0", provider.ivChecks)
	}
}
