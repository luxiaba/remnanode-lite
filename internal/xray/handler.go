package xray

import (
	"context"
	"fmt"

	"github.com/Luxiaba/remnanode-lite/internal/xtls"
)

func (m *Manager) mutationHandlerAPI(ctx context.Context) (*xtls.HandlerAPI, *mutationToken, context.Context, func(), error) {
	token, leaseContext, release, err := m.mutationToken(ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	client, err := xtls.NewClient(token.socket)
	if err != nil {
		release()
		return nil, nil, nil, nil, err
	}
	api := xtls.NewHandlerAPI(client.Conn())
	return api, token, leaseContext, func() {
		_ = client.Close()
		release()
	}, nil
}

func (m *Manager) readHandlerAPI(ctx context.Context) (*xtls.HandlerAPI, func(), error) {
	process, err := m.processForRPC(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	client, err := xtls.NewClient(process.socket)
	if err != nil {
		return nil, nil, err
	}
	return xtls.NewHandlerAPI(client.Conn()), func() { _ = client.Close() }, nil
}

func (m *Manager) HandlerAddVlessUser(ctx context.Context, tag, username, uuid, flow string, level uint32, hashUUID string) xtls.HandlerResult {
	api, token, rpcContext, closeFn, err := m.mutationHandlerAPI(ctx)
	if err != nil {
		return xtls.HandlerResult{OK: false, Message: err.Error()}
	}
	defer closeFn()
	return m.commitAddedResult(token, api.AddVlessUser(rpcContext, tag, username, uuid, flow, level), tag, hashUUID)
}

func (m *Manager) HandlerAddTrojanUser(ctx context.Context, tag, username, password string, level uint32, hashUUID string) xtls.HandlerResult {
	api, token, rpcContext, closeFn, err := m.mutationHandlerAPI(ctx)
	if err != nil {
		return xtls.HandlerResult{OK: false, Message: err.Error()}
	}
	defer closeFn()
	return m.commitAddedResult(token, api.AddTrojanUser(rpcContext, tag, username, password, level), tag, hashUUID)
}

func (m *Manager) HandlerAddShadowsocksUser(ctx context.Context, tag, username, password string, cipherType int, ivCheck bool, level uint32, hashUUID string) xtls.HandlerResult {
	api, token, rpcContext, closeFn, err := m.mutationHandlerAPI(ctx)
	if err != nil {
		return xtls.HandlerResult{OK: false, Message: err.Error()}
	}
	defer closeFn()
	return m.commitAddedResult(token, api.AddShadowsocksUser(rpcContext, tag, username, password, cipherType, ivCheck, level), tag, hashUUID)
}

func (m *Manager) HandlerAddShadowsocks2022User(ctx context.Context, tag, username, key string, level uint32, hashUUID string) xtls.HandlerResult {
	api, token, rpcContext, closeFn, err := m.mutationHandlerAPI(ctx)
	if err != nil {
		return xtls.HandlerResult{OK: false, Message: err.Error()}
	}
	defer closeFn()
	return m.commitAddedResult(token, api.AddShadowsocks2022User(rpcContext, tag, username, key, level), tag, hashUUID)
}

func (m *Manager) HandlerAddHysteriaUser(ctx context.Context, tag, username, auth string, level uint32, hashUUID string) xtls.HandlerResult {
	api, token, rpcContext, closeFn, err := m.mutationHandlerAPI(ctx)
	if err != nil {
		return xtls.HandlerResult{OK: false, Message: err.Error()}
	}
	defer closeFn()
	return m.commitAddedResult(token, api.AddHysteriaUser(rpcContext, tag, username, auth, level), tag, hashUUID)
}

func (m *Manager) HandlerRemoveOutbound(ctx context.Context, tag string) error {
	api, _, rpcContext, closeFn, err := m.mutationHandlerAPI(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	return api.RemoveOutbound(rpcContext, tag)
}

func (m *Manager) HandlerRemoveUser(ctx context.Context, tag, username, hashUUID string) xtls.HandlerResult {
	api, token, rpcContext, closeFn, err := m.mutationHandlerAPI(ctx)
	if err != nil {
		return xtls.HandlerResult{OK: false, Message: err.Error()}
	}
	defer closeFn()
	return m.commitRemovedResult(token, api.RemoveUser(rpcContext, tag, username), tag, hashUUID)
}

func (m *Manager) HandlerGetInboundUsers(ctx context.Context, tag string) ([]xtls.InboundUser, xtls.HandlerResult) {
	api, closeFn, err := m.readHandlerAPI(ctx)
	if err != nil {
		return nil, xtls.HandlerResult{OK: false, Message: err.Error()}
	}
	defer closeFn()
	return api.GetInboundUsers(ctx, tag)
}

func (m *Manager) HandlerGetInboundUsersCount(ctx context.Context, tag string) (int64, xtls.HandlerResult) {
	api, closeFn, err := m.readHandlerAPI(ctx)
	if err != nil {
		return 0, xtls.HandlerResult{OK: false, Message: err.Error()}
	}
	defer closeFn()
	return api.GetInboundUsersCount(ctx, tag)
}

func (m *Manager) commitAddedResult(token *mutationToken, result xtls.HandlerResult, tag, hashUUID string) xtls.HandlerResult {
	if result.OK && !m.commitUserAdded(token, tag, hashUUID) {
		return xtls.HandlerResult{OK: false, Message: "Xray lifecycle changed before user state commit"}
	}
	return result
}

func (m *Manager) commitRemovedResult(token *mutationToken, result xtls.HandlerResult, tag, hashUUID string) xtls.HandlerResult {
	if result.OK && !m.commitUserRemoved(token, tag, hashUUID) {
		return xtls.HandlerResult{OK: false, Message: "Xray lifecycle changed before user state commit"}
	}
	return result
}

func (m *Manager) RemoveTorrentBlockerOutbound() error {
	m.mu.RLock()
	online := m.state == lifecycleRunning
	m.mu.RUnlock()
	if !online {
		return nil
	}
	ctx, release, err := m.BeginMutation(context.Background())
	if err != nil {
		return err
	}
	defer release()
	return m.HandlerRemoveOutbound(ctx, torrentBlockerOutboundTag)
}

func (m *Manager) StopIfOnline() error {
	m.mu.RLock()
	stopped := m.state == lifecycleStopped
	m.mu.RUnlock()
	if stopped {
		return nil
	}
	if !m.Stop().IsStopped {
		return fmt.Errorf("stop rw-core: process did not stop")
	}
	return nil
}
