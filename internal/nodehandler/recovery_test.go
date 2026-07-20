package nodehandler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Luxiaba/remnanode-lite/internal/nodeapi"
	"github.com/Luxiaba/remnanode-lite/internal/xtls"
)

type panicRecoveryProvider struct {
	panicValue any
	releases   atomic.Int32
}

func (p *panicRecoveryProvider) BeginMutation(ctx context.Context) (context.Context, func(), error) {
	return ctx, func() { p.releases.Add(1) }, nil
}

func (p *panicRecoveryProvider) InboundTags() []string {
	if p.panicValue != nil {
		panic(p.panicValue)
	}
	return []string{"in-1"}
}

func (*panicRecoveryProvider) GetUserIPList(context.Context, string, bool) ([]xtls.IPEntry, error) {
	return nil, nil
}

func (*panicRecoveryProvider) HandlerRemoveUser(context.Context, string, string, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*panicRecoveryProvider) HandlerAddVlessUser(context.Context, string, string, string, string, uint32, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*panicRecoveryProvider) HandlerAddTrojanUser(context.Context, string, string, string, uint32, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*panicRecoveryProvider) HandlerAddShadowsocksUser(context.Context, string, string, string, int, bool, uint32, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*panicRecoveryProvider) HandlerAddShadowsocks2022User(context.Context, string, string, string, uint32, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*panicRecoveryProvider) HandlerAddHysteriaUser(context.Context, string, string, string, uint32, string) xtls.HandlerResult {
	return xtls.HandlerResult{OK: true}
}

func (*panicRecoveryProvider) HandlerGetInboundUsers(context.Context, string) ([]xtls.InboundUser, xtls.HandlerResult) {
	return nil, xtls.HandlerResult{OK: true}
}

func (*panicRecoveryProvider) HandlerGetInboundUsersCount(context.Context, string) (int64, xtls.HandlerResult) {
	return 0, xtls.HandlerResult{OK: true}
}

type capturedLogRecord struct {
	message string
	attrs   map[string]any
}

type capturedLogHandler struct {
	mu      sync.Mutex
	records []capturedLogRecord
}

func (*capturedLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturedLogHandler) Handle(_ context.Context, record slog.Record) error {
	entry := capturedLogRecord{message: record.Message, attrs: make(map[string]any)}
	record.Attrs(func(attr slog.Attr) bool {
		entry.attrs[attr.Key] = attr.Value.Any()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, entry)
	h.mu.Unlock()
	return nil
}

func (h *capturedLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturedLogHandler) WithGroup(string) slog.Handler      { return h }

func (h *capturedLogHandler) snapshot() []capturedLogRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]capturedLogRecord(nil), h.records...)
}

func TestMutationPanicIsObservableWithoutLeakingOrLockingGate(t *testing.T) {
	const panicValue = "sensitive-panic-diagnostic"
	tests := []struct {
		name      string
		operation string
		call      func(*Service) (GenericResponse, error)
	}{
		{
			name:      "add user",
			operation: "AddUser",
			call: func(service *Service) (GenericResponse, error) {
				return service.AddUser(context.Background(), AddUserRequest{
					Data:     []AddUserItem{{Type: "vless", Tag: "in-1", Username: "u1", UUID: "uuid-1"}},
					HashData: AddUserHashData{VlessUUID: "hash-1"},
				})
			},
		},
		{
			name:      "remove user",
			operation: "RemoveUser",
			call: func(service *Service) (GenericResponse, error) {
				return service.RemoveUser(context.Background(), RemoveUserRequest{Username: "u1", VlessUUID: "hash-1"})
			},
		},
		{
			name:      "add users",
			operation: "AddUsers",
			call: func(service *Service) (GenericResponse, error) {
				return service.AddUsers(context.Background(), AddUsersRequest{
					AffectedInboundTags: []string{"in-1"},
					Users: []BatchUser{{
						InboundData: []BatchInbound{{Type: "vless", Tag: "in-1"}},
						UserData:    BatchUserData{UserID: "u1", HashUUID: "hash-1", VlessUUID: "uuid-1"},
					}},
				})
			},
		},
		{
			name:      "remove users",
			operation: "RemoveUsers",
			call: func(service *Service) (GenericResponse, error) {
				return service.RemoveUsers(context.Background(), RemoveUsersRequest{
					Users: []RemoveUsersItem{{UserID: "u1", HashUUID: "hash-1"}},
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &panicRecoveryProvider{panicValue: panicValue}
			logs := &capturedLogHandler{}
			service := NewService(provider, nil)
			service.panicReporter = newPanicReporter(slog.New(logs))

			response, err := test.call(service)
			serviceError, ok := nodeapi.AsServiceError(err)
			if !ok || serviceError.Code != "A001" || serviceError.Message != "Server error" || serviceError.Status != 500 {
				t.Fatalf("error = %#v, want A001 Server error", err)
			}
			visible := fmt.Sprintf("%+v %v", response, err)
			if strings.Contains(visible, panicValue) {
				t.Fatalf("client-visible result leaks panic value: %s", visible)
			}
			if got := provider.releases.Load(); got != 1 {
				t.Fatalf("core mutation lease releases after panic = %d, want 1", got)
			}

			records := logs.snapshot()
			if len(records) != 1 {
				t.Fatalf("panic log count = %d, want 1", len(records))
			}
			record := records[0]
			if record.message != "recovered panic in node mutation" {
				t.Fatalf("panic log message = %q", record.message)
			}
			if got := record.attrs["operation"]; got != test.operation {
				t.Fatalf("panic operation = %v, want %s", got, test.operation)
			}
			if got := record.attrs["panic"]; got != panicValue {
				t.Fatalf("panic diagnostic = %v, want %q", got, panicValue)
			}
			stack, ok := record.attrs["stack"].(string)
			if !ok || stack == "" || len(stack) > maxPanicStackBytes || !strings.Contains(stack, "InboundTags") {
				t.Fatalf("panic stack is missing or invalid: type=%T bytes=%d", record.attrs["stack"], len(stack))
			}

			provider.panicValue = nil
			type result struct {
				response GenericResponse
				err      error
			}
			done := make(chan result, 1)
			go func() {
				secondResponse, secondErr := test.call(service)
				done <- result{response: secondResponse, err: secondErr}
			}()
			select {
			case second := <-done:
				if second.err != nil || !second.response.Success {
					t.Fatalf("mutation after panic = (%+v, %v), want success", second.response, second.err)
				}
				if got := provider.releases.Load(); got != 2 {
					t.Fatalf("core mutation lease releases after recovery = %d, want 2", got)
				}
			case <-time.After(time.Second):
				t.Fatal("mutation gate remained locked after panic")
			}
		})
	}
}

func TestPanicReporterBoundsAndRateLimitsDiagnostics(t *testing.T) {
	logs := &capturedLogHandler{}
	reporter := newPanicReporter(slog.New(logs))
	now := time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)
	reporter.now = func() time.Time { return now }

	longValue := strings.Repeat("界", maxPanicValueBytes)
	longStack := []byte(strings.Repeat("stack\n", maxPanicStackBytes))
	for range panicLogBurst + 2 {
		reporter.report(mutationAddUser, longValue, longStack)
	}
	records := logs.snapshot()
	if len(records) != panicLogBurst {
		t.Fatalf("logs in one window = %d, want %d", len(records), panicLogBurst)
	}
	panicText, ok := records[0].attrs["panic"].(string)
	if !ok || len(panicText) > maxPanicValueBytes || !utf8.ValidString(panicText) || !strings.HasSuffix(panicText, "... [truncated]") {
		t.Fatalf("bounded panic value is invalid: type=%T bytes=%d", records[0].attrs["panic"], len(panicText))
	}
	stack, ok := records[0].attrs["stack"].(string)
	if !ok || len(stack) > maxPanicStackBytes || !strings.HasSuffix(stack, "... [truncated]") {
		t.Fatalf("bounded stack is invalid: type=%T bytes=%d", records[0].attrs["stack"], len(stack))
	}

	now = now.Add(panicLogWindow)
	reporter.report(mutationAddUser, "next panic", []byte("next stack"))
	records = logs.snapshot()
	if len(records) != panicLogBurst+1 {
		t.Fatalf("logs after new window = %d, want %d", len(records), panicLogBurst+1)
	}
	if got := records[len(records)-1].attrs["suppressed"]; got != uint64(2) {
		t.Fatalf("suppressed panic count = %v, want 2", got)
	}
}
