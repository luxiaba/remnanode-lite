package xray

import (
	"context"

	"github.com/luxiaba/remnanode-lite/internal/xrayrpc"
)

func (m *Manager) statsAPI(ctx context.Context, requireOnline bool) (*xrayrpc.StatsAPI, func(), error) {
	process, err := m.processForRPC(ctx, requireOnline)
	if err != nil {
		return nil, nil, err
	}
	client, err := xrayrpc.NewClient(process.socket)
	if err != nil {
		return nil, nil, err
	}

	api := xrayrpc.NewStatsAPI(client.Conn(), &process.statsCapabilities)
	return api, func() { _ = client.Close() }, nil
}

func (m *Manager) pingProcess(ctx context.Context, process *processState) bool {
	if process == nil || process.socket == "" {
		return false
	}
	client, err := xrayrpc.NewClient(process.socket)
	if err != nil {
		return false
	}
	defer client.Close()
	return xrayrpc.NewStatsAPI(client.Conn(), &process.statsCapabilities).Ping(ctx) == nil
}

func (m *Manager) PingXrayGRPC(ctx context.Context) bool {
	api, closeFn, err := m.statsAPI(ctx, false)
	if err != nil {
		return false
	}
	defer closeFn()
	return api.Ping(ctx) == nil
}

func (m *Manager) GetSysStats(ctx context.Context) (*xrayrpc.SysStats, error) {
	api, closeFn, err := m.statsAPI(ctx, true)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	return api.GetSysStats(ctx)
}

func (m *Manager) GetAllUsersStats(ctx context.Context, reset bool) ([]xrayrpc.UserTraffic, error) {
	ctx, release, err := m.statsMutationContext(ctx, reset)
	if err != nil {
		return nil, err
	}
	defer release()
	api, closeFn, err := m.statsAPI(ctx, true)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	return api.GetAllUsersStats(ctx, reset)
}

func (m *Manager) GetUserOnlineStatus(ctx context.Context, username string) (bool, error) {
	api, closeFn, err := m.statsAPI(ctx, true)
	if err != nil {
		return false, err
	}
	defer closeFn()
	return api.GetUserOnlineStatus(ctx, username)
}

func (m *Manager) GetInboundStats(ctx context.Context, tag string, reset bool) (xrayrpc.TagTraffic, error) {
	ctx, release, err := m.statsMutationContext(ctx, reset)
	if err != nil {
		return xrayrpc.TagTraffic{}, err
	}
	defer release()
	api, closeFn, err := m.statsAPI(ctx, true)
	if err != nil {
		return xrayrpc.TagTraffic{}, err
	}
	defer closeFn()
	return api.GetInboundStats(ctx, tag, reset)
}

func (m *Manager) GetOutboundStats(ctx context.Context, tag string, reset bool) (xrayrpc.TagTraffic, error) {
	ctx, release, err := m.statsMutationContext(ctx, reset)
	if err != nil {
		return xrayrpc.TagTraffic{}, err
	}
	defer release()
	api, closeFn, err := m.statsAPI(ctx, true)
	if err != nil {
		return xrayrpc.TagTraffic{}, err
	}
	defer closeFn()
	return api.GetOutboundStats(ctx, tag, reset)
}

func (m *Manager) GetAllInboundsStats(ctx context.Context, reset bool) ([]xrayrpc.TagTraffic, error) {
	ctx, release, err := m.statsMutationContext(ctx, reset)
	if err != nil {
		return nil, err
	}
	defer release()
	api, closeFn, err := m.statsAPI(ctx, true)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	return api.GetAllInboundsStats(ctx, reset)
}

func (m *Manager) GetAllOutboundsStats(ctx context.Context, reset bool) ([]xrayrpc.TagTraffic, error) {
	ctx, release, err := m.statsMutationContext(ctx, reset)
	if err != nil {
		return nil, err
	}
	defer release()
	api, closeFn, err := m.statsAPI(ctx, true)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	return api.GetAllOutboundsStats(ctx, reset)
}

func (m *Manager) GetUserIPList(ctx context.Context, userID string, reset bool) ([]xrayrpc.IPEntry, error) {
	ctx, release, err := m.statsMutationContext(ctx, reset)
	if err != nil {
		return nil, err
	}
	defer release()
	api, closeFn, err := m.statsAPI(ctx, true)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	return api.GetUserIPList(ctx, userID, reset)
}

func (m *Manager) statsMutationContext(ctx context.Context, reset bool) (context.Context, func(), error) {
	if !reset {
		if ctx == nil {
			ctx = context.Background()
		}
		return ctx, func() {}, nil
	}
	_, leaseContext, release, err := m.mutationToken(ctx)
	return leaseContext, release, err
}

func (m *Manager) GetUsersIPList(ctx context.Context) ([]xrayrpc.UserIPEntry, error) {
	api, closeFn, err := m.statsAPI(ctx, true)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	return api.GetUsersIPList(ctx)
}
