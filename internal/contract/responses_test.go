package contract_test

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/luxiaba/remnanode-lite/internal/xrayrpc"
)

type stubHandlerProvider struct{}

func (stubHandlerProvider) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	return ctx, func() {}, nil
}
func (stubHandlerProvider) InboundTags() []string { return nil }
func (stubHandlerProvider) GetUserIPList(context.Context, string, bool) ([]xrayrpc.IPEntry, error) {
	return nil, nil
}
func (stubHandlerProvider) HandlerRemoveUser(context.Context, string, string, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: true}
}
func (stubHandlerProvider) HandlerAddVlessUser(context.Context, string, string, string, string, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: false, Message: "offline"}
}
func (stubHandlerProvider) HandlerAddTrojanUser(context.Context, string, string, string, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: false}
}
func (stubHandlerProvider) HandlerAddShadowsocksUser(context.Context, string, string, string, int, bool, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: false}
}
func (stubHandlerProvider) HandlerAddShadowsocks2022User(context.Context, string, string, string, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: false}
}
func (stubHandlerProvider) HandlerAddHysteriaUser(context.Context, string, string, string, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: false}
}
func (stubHandlerProvider) HandlerGetInboundUsers(context.Context, string) ([]xrayrpc.InboundUser, xrayrpc.HandlerResult) {
	return []xrayrpc.InboundUser{{Username: "user-1", Level: 1, Protocol: "vless"}}, xrayrpc.HandlerResult{OK: true}
}
func (stubHandlerProvider) HandlerGetInboundUsersCount(context.Context, string) (int64, xrayrpc.HandlerResult) {
	return 0, xrayrpc.HandlerResult{OK: true}
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
