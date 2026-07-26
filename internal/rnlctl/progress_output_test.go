package rnlctl

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type progressTestClock struct {
	now time.Time
}

func newProgressTestClock() *progressTestClock {
	return &progressTestClock{now: time.Unix(1_700_000_000, 0)}
}

func (clock *progressTestClock) Now() time.Time {
	return clock.now
}

func (clock *progressTestClock) Advance(duration time.Duration) {
	clock.now = clock.now.Add(duration)
}

func TestProgressRendererSelectsInteractivePlainAndNeverModes(t *testing.T) {
	tests := []struct {
		name       string
		mode       progressMode
		terminal   bool
		wantMode   progressMode
		wantOutput string
		wantTTY    bool
	}{
		{name: "auto terminal", mode: progressAuto, terminal: true, wantMode: progressAuto, wantOutput: "[OK]", wantTTY: true},
		{name: "auto redirected", mode: progressAuto, wantMode: progressPlain, wantOutput: "rnlctl: upgrade: Verify bundle and manifest"},
		{name: "forced plain terminal", mode: progressPlain, terminal: true, wantMode: progressPlain, wantOutput: "rnlctl: upgrade: Verify bundle and manifest"},
		{name: "never", mode: progressNever, terminal: true, wantMode: progressNever},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newProgressTestClock()
			var output bytes.Buffer
			renderer := newProgressRenderer(progressRendererOptions{
				Writer: &output,
				Mode:   test.mode,
				IsTerminal: func(io.Writer) bool {
					return test.terminal
				},
				TerminalWidth: func(io.Writer) int { return 64 },
				Now:           clock.Now,
			})
			if renderer.mode != test.wantMode {
				t.Fatalf("renderer mode = %q, want %q", renderer.mode, test.wantMode)
			}

			renderer.Emit(progressEvent{
				Kind: progressPhaseStarted, Operation: "upgrade", Phase: phaseVerifyBundle,
			})
			renderer.Finish(true)

			got := output.String()
			if test.wantOutput == "" {
				if got != "" {
					t.Fatalf("output = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, test.wantOutput) {
				t.Fatalf("output = %q, want %q", got, test.wantOutput)
			}
			if strings.Contains(got, "\r\x1b[2K") != test.wantTTY {
				t.Fatalf("terminal controls in output = %q, want %t", got, test.wantTTY)
			}
		})
	}
}

func TestProgressRendererPlainKnownTransferUsesStableMilestones(t *testing.T) {
	var output bytes.Buffer
	renderer := newProgressRenderer(progressRendererOptions{Writer: &output, Mode: progressPlain})
	total := int64(40 << 20)
	renderer.Emit(progressEvent{Kind: progressPhaseStarted, Operation: "upgrade", Phase: phaseDownloadBundle})
	for _, current := range []int64{10 << 20, 20 << 20, 21 << 20, 40 << 20} {
		renderer.Emit(progressEvent{
			Kind: progressTransferUpdated, Operation: "upgrade", Phase: phaseDownloadBundle,
			Current: current, Total: total,
		})
	}
	renderer.Finish(true)

	got := output.String()
	for _, want := range []string{
		"rnlctl: upgrade: Download Native bundle\n",
		"download 25% (10.0 MiB/40.0 MiB)",
		"download 50% (20.0 MiB/40.0 MiB)",
		"download 100% (40.0 MiB/40.0 MiB)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("plain output = %q, want %q", got, want)
		}
	}
	if strings.ContainsAny(got, "\r\x1b") {
		t.Fatalf("plain output contains terminal controls: %q", got)
	}
	if count := strings.Count(got, "download 50%"); count != 1 {
		t.Fatalf("50%% milestone count = %d, output = %q", count, got)
	}
}

func TestProgressRendererPlainUnknownTransferUsesBoundedByteMilestones(t *testing.T) {
	var output bytes.Buffer
	renderer := newProgressRenderer(progressRendererOptions{Writer: &output, Mode: progressPlain})
	renderer.Emit(progressEvent{Kind: progressPhaseStarted, Operation: "upgrade", Phase: phaseDownloadBundle})
	for _, current := range []int64{8 << 20, 16 << 20, 24 << 20, 32 << 20} {
		renderer.Emit(progressEvent{
			Kind: progressTransferUpdated, Operation: "upgrade", Phase: phaseDownloadBundle,
			Current: current, Total: -1,
		})
	}
	renderer.Finish(true)

	got := output.String()
	for _, want := range []string{"downloaded 16.0 MiB", "downloaded 32.0 MiB"} {
		if !strings.Contains(got, want) {
			t.Fatalf("plain output = %q, want %q", got, want)
		}
	}
	for _, unwanted := range []string{"downloaded 8.0 MiB", "downloaded 24.0 MiB", "%", "ETA"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("plain output = %q, unexpectedly contains %q", got, unwanted)
		}
	}
}

func TestProgressRendererInteractiveKnownAndUnknownTransfers(t *testing.T) {
	t.Run("known", func(t *testing.T) {
		clock := newProgressTestClock()
		var output bytes.Buffer
		renderer := newProgressRenderer(progressRendererOptions{
			Writer: &output, Mode: progressAuto,
			IsTerminal:    func(io.Writer) bool { return true },
			TerminalWidth: func(io.Writer) int { return 72 },
			Now:           clock.Now,
		})
		renderer.Emit(progressEvent{
			Kind: progressPhaseStarted, Operation: "upgrade", Phase: phaseDownloadBundle,
		})
		clock.Advance(interactiveDrawInterval)
		renderer.Emit(progressEvent{
			Kind: progressTransferUpdated, Operation: "upgrade", Phase: phaseDownloadBundle,
			Current: 32 << 20, Total: 64 << 20,
		})
		clock.Advance(2 * time.Second)
		renderer.Emit(progressEvent{
			Kind: progressTransferUpdated, Operation: "upgrade", Phase: phaseDownloadBundle,
			Current: 64 << 20, Total: 64 << 20,
		})

		got := output.String()
		for _, want := range []string{" 50%", "100%", "64.0 MiB/64.0 MiB", "/s"} {
			if !strings.Contains(got, want) {
				t.Fatalf("interactive output = %q, want %q", got, want)
			}
		}
	})

	t.Run("unknown", func(t *testing.T) {
		clock := newProgressTestClock()
		var output bytes.Buffer
		renderer := newProgressRenderer(progressRendererOptions{
			Writer: &output, Mode: progressAuto,
			IsTerminal:    func(io.Writer) bool { return true },
			TerminalWidth: func(io.Writer) int { return 72 },
			Now:           clock.Now,
		})
		renderer.Emit(progressEvent{
			Kind: progressPhaseStarted, Operation: "upgrade", Phase: phaseDownloadBundle,
		})
		clock.Advance(2 * time.Second)
		renderer.Emit(progressEvent{
			Kind: progressTransferUpdated, Operation: "upgrade", Phase: phaseDownloadBundle,
			Current: 20 << 20, Total: -1,
		})

		lastFrame := output.String()[strings.LastIndex(output.String(), "\r\x1b[2K"):]
		if !strings.Contains(lastFrame, "20.0 MiB") || !strings.Contains(lastFrame, "/s") {
			t.Fatalf("unknown-length frame = %q", lastFrame)
		}
		if strings.Contains(lastFrame, "%") || strings.Contains(lastFrame, "ETA") {
			t.Fatalf("unknown-length frame invents total-based metrics: %q", lastFrame)
		}
	})
}

func TestProgressRendererThrottlesIntermediateFramesButFlushesCompletion(t *testing.T) {
	clock := newProgressTestClock()
	var output bytes.Buffer
	renderer := newProgressRenderer(progressRendererOptions{
		Writer: &output, Mode: progressAuto,
		IsTerminal:    func(io.Writer) bool { return true },
		TerminalWidth: func(io.Writer) int { return 72 },
		Now:           clock.Now,
	})
	renderer.Emit(progressEvent{
		Kind: progressPhaseStarted, Operation: "upgrade", Phase: phaseDownloadBundle,
	})
	initialFrames := strings.Count(output.String(), "\r\x1b[2K")

	renderer.Emit(progressEvent{
		Kind: progressTransferUpdated, Operation: "upgrade", Phase: phaseDownloadBundle,
		Current: 8 << 20, Total: 64 << 20,
	})
	clock.Advance(interactiveDrawInterval - time.Millisecond)
	renderer.Emit(progressEvent{
		Kind: progressPhaseHeartbeat, Operation: "upgrade", Phase: phaseDownloadBundle,
	})
	if frames := strings.Count(output.String(), "\r\x1b[2K"); frames != initialFrames {
		t.Fatalf("frames inside throttle interval = %d, want %d; output = %q", frames, initialFrames, output.String())
	}

	clock.Advance(time.Millisecond)
	renderer.Emit(progressEvent{
		Kind: progressPhaseHeartbeat, Operation: "upgrade", Phase: phaseDownloadBundle,
	})
	if frames := strings.Count(output.String(), "\r\x1b[2K"); frames != initialFrames+1 {
		t.Fatalf("frames after throttle interval = %d, want %d; output = %q", frames, initialFrames+1, output.String())
	}

	renderer.Emit(progressEvent{
		Kind: progressTransferUpdated, Operation: "upgrade", Phase: phaseDownloadBundle,
		Current: 64 << 20, Total: 64 << 20,
	})
	if frames := strings.Count(output.String(), "\r\x1b[2K"); frames != initialFrames+2 {
		t.Fatalf("completion frames = %d, want %d; output = %q", frames, initialFrames+2, output.String())
	}
}

func TestProgressRendererRespectsInjectedTerminalWidth(t *testing.T) {
	var output bytes.Buffer
	renderer := newProgressRenderer(progressRendererOptions{
		Writer: &output, Mode: progressAuto,
		IsTerminal:    func(io.Writer) bool { return true },
		TerminalWidth: func(io.Writer) int { return 36 },
		Now:           newProgressTestClock().Now,
	})
	renderer.Emit(progressEvent{Kind: progressPhaseStarted, Operation: "upgrade", Phase: phaseDownloadBundle})
	renderer.Emit(progressEvent{
		Kind: progressTransferUpdated, Operation: "upgrade", Phase: phaseDownloadBundle,
		Current: 48 << 20, Total: 64 << 20,
	})

	got := output.String()
	lastClear := strings.LastIndex(got, "\r\x1b[2K")
	if lastClear < 0 {
		t.Fatalf("interactive output has no clear sequence: %q", got)
	}
	frame := got[lastClear+len("\r\x1b[2K"):]
	if len(frame) > 36 {
		t.Fatalf("frame width = %d, want <= 36; frame = %q", len(frame), frame)
	}
}

func TestProgressRendererRespectsVeryNarrowTerminalWidth(t *testing.T) {
	for _, width := range []int{3, 12} {
		var output bytes.Buffer
		renderer := newProgressRenderer(progressRendererOptions{
			Writer: &output, Mode: progressAuto,
			IsTerminal:    func(io.Writer) bool { return true },
			TerminalWidth: func(io.Writer) int { return width },
			Now:           newProgressTestClock().Now,
		})
		renderer.Emit(progressEvent{Kind: progressPhaseStarted, Operation: "upgrade", Phase: phaseDownloadBundle})

		frame := output.String()[strings.LastIndex(output.String(), "\r\x1b[2K")+len("\r\x1b[2K"):]
		if len(frame) > width {
			t.Fatalf("width %d rendered %q (%d bytes)", width, frame, len(frame))
		}
	}
}

func TestProgressRendererIgnoresOrphanHeartbeatAndTransferEvents(t *testing.T) {
	var output bytes.Buffer
	renderer := newProgressRenderer(progressRendererOptions{
		Writer: &output, Mode: progressAuto,
		IsTerminal: func(io.Writer) bool { return true },
	})
	renderer.Emit(progressEvent{Kind: progressPhaseHeartbeat, Operation: "status", Phase: phaseWaitHealthy})
	renderer.Emit(progressEvent{
		Kind: progressTransferUpdated, Operation: "status", Phase: phaseDownloadBundle,
		Current: 1, Total: 2,
	})
	renderer.Finish(true)

	if output.Len() != 0 {
		t.Fatalf("orphan event output = %q, want empty", output.String())
	}
}

func TestProgressRendererClearsInteractiveLineBeforeFailure(t *testing.T) {
	var output bytes.Buffer
	renderer := newProgressRenderer(progressRendererOptions{
		Writer: &output, Mode: progressAuto,
		IsTerminal:    func(io.Writer) bool { return true },
		TerminalWidth: func(io.Writer) int { return 64 },
		Now:           newProgressTestClock().Now,
	})
	renderer.Emit(progressEvent{
		Kind: progressPhaseStarted, Operation: "restart", Phase: phaseWaitHealthy,
	})
	renderer.Emit(progressEvent{
		Kind: progressPhaseHeartbeat, Operation: "restart", Phase: phaseWaitHealthy,
	})
	renderer.Finish(false)

	got := output.String()
	if !strings.Contains(got, "\r\x1b[2K[FAIL] Wait for runtime health\n") {
		t.Fatalf("failure output does not clear the active line first: %q", got)
	}
	if strings.Contains(got, "[OK]") {
		t.Fatalf("failure output contains a success marker: %q", got)
	}
}

func TestProgressRendererCanCompleteRecoveryBeforeOperationError(t *testing.T) {
	var output bytes.Buffer
	renderer := newProgressRenderer(progressRendererOptions{
		Writer: &output, Mode: progressAuto,
		IsTerminal:    func(io.Writer) bool { return true },
		TerminalWidth: func(io.Writer) int { return 64 },
		Now:           newProgressTestClock().Now,
	})
	renderer.Emit(progressEvent{Kind: progressPhaseStarted, Operation: "upgrade", Phase: phaseWaitHealthy})
	renderer.Emit(progressEvent{Kind: progressActivePhaseCompleted, Operation: "upgrade", Success: false})
	renderer.Emit(progressEvent{Kind: progressPhaseStarted, Operation: "upgrade", Phase: phaseRestorePrevious})
	renderer.Emit(progressEvent{
		Kind: progressPhaseCompleted, Operation: "upgrade", Phase: phaseRestorePrevious, Success: true,
	})
	renderer.Finish(false)

	got := output.String()
	if !strings.Contains(got, "[FAIL] Wait for runtime health") ||
		!strings.Contains(got, "[OK] Restore previous working state") ||
		strings.Contains(got, "[FAIL] Restore previous working state") {
		t.Fatalf("completed recovery output = %q", got)
	}
}

type progressRejectingWriter struct {
	calls int
}

func (writer *progressRejectingWriter) Write([]byte) (int, error) {
	writer.calls++
	return 0, errors.New("write rejected")
}

func TestProgressRendererDisablesOutputAfterWriterFailure(t *testing.T) {
	writer := &progressRejectingWriter{}
	renderer := newProgressRenderer(progressRendererOptions{
		Writer: writer, Mode: progressPlain,
	})
	renderer.Emit(progressEvent{
		Kind: progressPhaseStarted, Operation: "upgrade", Phase: phaseVerifyBundle,
	})
	renderer.Emit(progressEvent{
		Kind: progressTransferUpdated, Operation: "upgrade", Phase: phaseDownloadBundle,
		Current: 32 << 20, Total: 64 << 20,
	})
	renderer.Finish(false)

	if !renderer.writeFailed {
		t.Fatal("renderer did not record the writer failure")
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls = %d, want 1 after output is disabled", writer.calls)
	}
}

type recordingProgressSink struct {
	events []progressEvent
}

func (sink *recordingProgressSink) Emit(event progressEvent) {
	sink.events = append(sink.events, event)
}

func TestProgressContextAddsOperationAndNormalizesTransfer(t *testing.T) {
	sink := &recordingProgressSink{}
	ctx := withProgressSink(nil, "upgrade", sink)
	emitProgressTransfer(ctx, phaseDownloadBundle, -1, -1)

	if len(sink.events) != 1 {
		t.Fatalf("events = %#v, want one", sink.events)
	}
	got := sink.events[0]
	if got.Operation != "upgrade" || got.Kind != progressTransferUpdated || got.Current != 0 || got.Total != -1 {
		t.Fatalf("event = %#v", got)
	}

	emitProgressPhase(nil, phaseVerifyBundle)
	emitProgressHeartbeat(withProgressSink(ctx, "", sink), phaseWaitHealthy)
	if len(sink.events) != 2 {
		t.Fatalf("events after nil/no-op contexts = %#v, want two total", sink.events)
	}
}

func TestProgressContextCanSuppressNestedRecoveryEvents(t *testing.T) {
	sink := &recordingProgressSink{}
	ctx := withProgressSink(context.Background(), "upgrade", sink)
	emitProgressPhase(ctx, phaseRestorePrevious)
	suppressedCtx := withProgressSuppressed(ctx)

	emitProgressPhase(suppressedCtx, phaseStartService)
	emitProgressHeartbeat(suppressedCtx, phaseWaitHealthy)
	completeProgressPhase(suppressedCtx, phaseStartService, true)
	completeProgressPhase(ctx, phaseRestorePrevious, true)

	if len(sink.events) != 2 {
		t.Fatalf("events = %#v, want only outer recovery start and completion", sink.events)
	}
	for _, event := range sink.events {
		if event.Phase != phaseRestorePrevious {
			t.Fatalf("event phase = %v, want recovery phase; events = %#v", event.Phase, sink.events)
		}
	}
}
