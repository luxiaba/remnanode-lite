package nodehandler

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxPanicValueBytes = 512
	maxPanicStackBytes = 8 << 10
	panicLogBurst      = 4
	panicLogWindow     = time.Minute
)

type mutationOperation uint8

const (
	mutationAddUser mutationOperation = iota
	mutationRemoveUser
	mutationAddUsers
	mutationRemoveUsers
	mutationOperationCount
)

func (o mutationOperation) String() string {
	switch o {
	case mutationAddUser:
		return "AddUser"
	case mutationRemoveUser:
		return "RemoveUser"
	case mutationAddUsers:
		return "AddUsers"
	case mutationRemoveUsers:
		return "RemoveUsers"
	default:
		return "unknown"
	}
}

type panicLogState struct {
	windowStart time.Time
	emitted     uint8
	suppressed  uint64
}

type panicReporter struct {
	mu     sync.Mutex
	logger *slog.Logger
	now    func() time.Time
	state  [mutationOperationCount]panicLogState
}

func newPanicReporter(logger *slog.Logger) *panicReporter {
	if logger == nil {
		logger = slog.Default()
	}
	return &panicReporter{logger: logger, now: time.Now}
}

func (s *Service) recoverServiceError(operation mutationOperation, err *error) {
	recovered := recover()
	if recovered == nil {
		return
	}
	*err = errInternalServer

	reporter := s.panicReporter
	if reporter == nil {
		reporter = newPanicReporter(nil)
	}
	reporter.report(operation, recovered, debug.Stack())
}

func (r *panicReporter) report(operation mutationOperation, recovered any, stack []byte) {
	if r == nil {
		return
	}

	now := time.Now()
	if r.now != nil {
		now = r.now()
	}
	index := int(operation)
	if index < 0 || index >= len(r.state) {
		index = 0
	}

	r.mu.Lock()
	state := &r.state[index]
	if state.windowStart.IsZero() || now.Sub(state.windowStart) >= panicLogWindow || now.Before(state.windowStart) {
		state.windowStart = now
		state.emitted = 0
	}
	if state.emitted >= panicLogBurst {
		state.suppressed++
		r.mu.Unlock()
		return
	}
	state.emitted++
	suppressed := state.suppressed
	state.suppressed = 0
	r.mu.Unlock()

	logger := r.logger
	if logger == nil {
		logger = slog.Default()
	}
	attributes := []any{
		"operation", operation.String(),
		"panicType", fmt.Sprintf("%T", recovered),
		"panic", boundPanicDiagnostic(fmt.Sprint(recovered), maxPanicValueBytes),
		"stack", boundPanicDiagnostic(string(stack), maxPanicStackBytes),
	}
	if suppressed != 0 {
		attributes = append(attributes, "suppressed", suppressed)
	}
	// Request data is deliberately excluded: panic diagnostics may be sensitive
	// and are intended only for the daemon's privileged log stream.
	logger.Error("recovered panic in node mutation", attributes...)
}

func boundPanicDiagnostic(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	const suffix = "... [truncated]"
	if limit <= len(suffix) {
		return suffix[:limit]
	}
	end := limit - len(suffix)
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end] + suffix
}
