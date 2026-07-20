package nodehandler_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/nodehandler"
	"github.com/luxiaba/remnanode-lite/internal/xrayrpc"
)

type cleanupFailureProvider struct {
	stubProvider
	addCalls       atomic.Int64
	removedCommits []string
}

func (p *cleanupFailureProvider) HandlerRemoveUser(_ context.Context, tag, _, hashUUID string) xrayrpc.HandlerResult {
	if tag == "in-2" {
		return xrayrpc.HandlerResult{OK: false, Message: "remove failed"}
	}
	p.removedCommits = append(p.removedCommits, tag+":"+hashUUID)
	return xrayrpc.HandlerResult{OK: true}
}

func (p *cleanupFailureProvider) HandlerAddVlessUser(context.Context, string, string, string, string, uint32, string) xrayrpc.HandlerResult {
	p.addCalls.Add(1)
	return xrayrpc.HandlerResult{OK: true}
}

func TestAddUsersStopsUserAfterCleanupFailure(t *testing.T) {
	t.Parallel()

	provider := &cleanupFailureProvider{stubProvider: stubProvider{inboundTags: []string{"in-1", "in-2"}}}
	service := nodehandler.NewService(provider, nil)
	response, err := service.AddUsers(context.Background(), batchVlessRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.Success || response.Error == nil || *response.Error != "remove failed" {
		t.Fatalf("response = %+v, want cleanup failure", response)
	}
	if provider.addCalls.Load() != 0 {
		t.Fatalf("add calls = %d, want zero after failed cleanup", provider.addCalls.Load())
	}
	if len(provider.removedCommits) != 1 || provider.removedCommits[0] != "in-1:h1" {
		t.Fatalf("removed commits = %#v, want only successful cleanup", provider.removedCommits)
	}
}

type partialAddProvider struct {
	stubProvider
	addedCommits []string
}

func (p *partialAddProvider) HandlerAddVlessUser(_ context.Context, tag, _, _, _ string, _ uint32, hashUUID string) xrayrpc.HandlerResult {
	p.addedCommits = append(p.addedCommits, tag+":"+hashUUID)
	return xrayrpc.HandlerResult{OK: true}
}

func (p *partialAddProvider) HandlerAddTrojanUser(context.Context, string, string, string, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: false, Message: "trojan failed"}
}

func TestAddUserReportsPartialFailureAndCommitsOnlySuccess(t *testing.T) {
	t.Parallel()

	provider := &partialAddProvider{}
	service := nodehandler.NewService(provider, nil)
	response, err := service.AddUser(context.Background(), nodehandler.AddUserRequest{
		Data: []nodehandler.AddUserItem{
			{Type: "vless", Tag: "in-1", Username: "u1", UUID: "uuid-1"},
			{Type: "trojan", Tag: "in-2", Username: "u1", Password: "secret"},
		},
		HashData: nodehandler.AddUserHashData{VlessUUID: "hash-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Success || response.Error == nil || *response.Error != "trojan failed" {
		t.Fatalf("response = %+v, want partial failure", response)
	}
	if len(provider.addedCommits) != 1 || provider.addedCommits[0] != "in-1:hash-1" {
		t.Fatalf("added commits = %#v, want only successful add", provider.addedCommits)
	}
}

type rejectedCommitProvider struct {
	stubProvider
}

func (p *rejectedCommitProvider) HandlerAddVlessUser(context.Context, string, string, string, string, uint32, string) xrayrpc.HandlerResult {
	return xrayrpc.HandlerResult{OK: false, Message: "Xray lifecycle changed before user state commit"}
}

func TestAddUserReportsProcessCommitRejection(t *testing.T) {
	t.Parallel()

	service := nodehandler.NewService(&rejectedCommitProvider{}, nil)
	response, err := service.AddUser(context.Background(), nodehandler.AddUserRequest{
		Data: []nodehandler.AddUserItem{{
			Type: "vless", Tag: "in-1", Username: "u1", UUID: "uuid-1",
		}},
		HashData: nodehandler.AddUserHashData{VlessUUID: "hash-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Success || response.Error == nil || *response.Error != "Xray lifecycle changed before user state commit" {
		t.Fatalf("response = %+v, want process commit failure", response)
	}
}

type blockingMutationProvider struct {
	stubProvider
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int64
}

func (p *blockingMutationProvider) HandlerRemoveUser(ctx context.Context, _, _, _ string) xrayrpc.HandlerResult {
	if p.calls.Add(1) == 1 {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
			return xrayrpc.HandlerResult{OK: false, Message: ctx.Err().Error()}
		}
	}
	return xrayrpc.HandlerResult{OK: true}
}

func TestCanceledMutationDoesNotEnterProviderWhileQueued(t *testing.T) {
	t.Parallel()

	provider := &blockingMutationProvider{
		stubProvider: stubProvider{inboundTags: []string{"in-1"}},
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	service := nodehandler.NewService(provider, nil)
	request := nodehandler.RemoveUserRequest{Username: "u1", VlessUUID: "hash-1"}
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = service.RemoveUser(context.Background(), request)
	}()

	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("first mutation did not enter provider")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.RemoveUser(ctx, request); err == nil {
		t.Fatal("canceled queued mutation must return an error")
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want one", provider.calls.Load())
	}

	close(provider.release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first mutation did not finish")
	}
}

func TestCanceledBatchStopsBeforeRemainingProviderCalls(t *testing.T) {
	t.Parallel()

	provider := &blockingMutationProvider{
		stubProvider: stubProvider{inboundTags: []string{"in-1", "in-2", "in-3"}},
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	service := nodehandler.NewService(provider, nil)
	ctx, cancel := context.WithCancel(context.Background())
	response := make(chan nodehandler.GenericResponse, 1)
	go func() {
		result, _ := service.AddUsers(ctx, batchVlessRequest())
		response <- result
	}()
	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("batch mutation did not enter provider")
	}
	cancel()
	select {
	case result := <-response:
		if result.Success || result.Error == nil || *result.Error != context.Canceled.Error() {
			t.Fatalf("canceled batch response = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled batch did not return")
	}
	if got := provider.calls.Load(); got != 1 {
		t.Fatalf("provider calls after cancellation = %d, want 1", got)
	}
}
