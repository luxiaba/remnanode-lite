package xray

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

var (
	errXrayOffline          = errors.New("xray is not online")
	errXrayLifecycleChanged = errors.New("xray lifecycle changed")
)

type mutationContextKey struct{}

type mutationToken struct {
	manager *Manager
	process *processState
	epoch   uint64
	socket  string
	active  atomic.Bool
}

// BeginMutation binds all rw-core calls made with the returned context to the
// current process. Start and Stop wait for the lease before replacing or
// terminating that process.
func (m *Manager) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if token, ok := ctx.Value(mutationContextKey{}).(*mutationToken); ok {
		if token != nil && token.manager == m && token.active.Load() {
			return ctx, func() {}, nil
		}
		return nil, nil, errXrayLifecycleChanged
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	m.mu.RLock()
	process := m.process
	eligible := m.state == lifecycleRunning && process != nil &&
		m.runtimeProcessEpoch == process.epoch
	m.mu.RUnlock()
	if !eligible {
		return nil, nil, errXrayOffline
	}

	// Never hold Manager.mu while waiting for the process gate. Lifecycle
	// writers publish a transient state first, then acquire this lock.
	process.mutationGate.RLock()
	m.mu.RLock()
	valid := m.state == lifecycleRunning && m.process == process &&
		m.runtimeProcessEpoch == process.epoch
	m.mu.RUnlock()
	if !valid {
		process.mutationGate.RUnlock()
		return nil, nil, errXrayLifecycleChanged
	}
	if err := ctx.Err(); err != nil {
		process.mutationGate.RUnlock()
		return nil, nil, err
	}

	token := &mutationToken{
		manager: m,
		process: process,
		epoch:   process.epoch,
		socket:  process.socket,
	}
	token.active.Store(true)
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			token.active.Store(false)
			process.mutationGate.RUnlock()
		})
	}
	return context.WithValue(ctx, mutationContextKey{}, token), release, nil
}

func (m *Manager) mutationToken(ctx context.Context) (*mutationToken, context.Context, func(), error) {
	if ctx != nil {
		if token, ok := ctx.Value(mutationContextKey{}).(*mutationToken); ok && token != nil {
			if token.manager != m || !token.active.Load() {
				return nil, nil, nil, errXrayLifecycleChanged
			}
			return token, ctx, func() {}, nil
		}
	}
	leaseContext, release, err := m.BeginMutation(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	token, _ := leaseContext.Value(mutationContextKey{}).(*mutationToken)
	if token == nil {
		release()
		return nil, nil, nil, errXrayLifecycleChanged
	}
	return token, leaseContext, release, nil
}

func (m *Manager) mutationTokenCurrentLocked(token *mutationToken) bool {
	return token != nil && token.manager == m && token.active.Load() && m.process == token.process &&
		m.runtimeProcessEpoch == token.epoch
}

func (m *Manager) processForRPC(ctx context.Context, requireOnline bool) (*processState, error) {
	if ctx != nil {
		if token, ok := ctx.Value(mutationContextKey{}).(*mutationToken); ok && token != nil {
			if token.manager != m || !token.active.Load() {
				return nil, errXrayLifecycleChanged
			}
			return token.process, nil
		}
	}

	m.mu.RLock()
	process := m.process
	online := m.state == lifecycleRunning && process != nil &&
		m.runtimeProcessEpoch == process.epoch
	m.mu.RUnlock()
	if requireOnline && !online {
		return nil, errXrayOffline
	}
	if process == nil {
		return nil, errXrayOffline
	}
	return process, nil
}
