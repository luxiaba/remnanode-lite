package rnlctl

import "context"

type progressEventKind uint8

const (
	progressPhaseStarted progressEventKind = iota + 1
	progressTransferUpdated
	progressPhaseHeartbeat
	progressPhaseCompleted
	progressActivePhaseCompleted
)

type operationPhase uint8

const (
	phaseResolveRelease operationPhase = iota + 1
	phaseDownloadChecksums
	phaseDownloadBundle
	phaseVerifyBundle
	phaseValidateHost
	phasePrepareGeneration
	phaseWriteConfiguration
	phasePrepareService
	phaseStopService
	phaseSwitchGeneration
	phaseStartService
	phaseRestartService
	phaseWaitHealthy
	phaseRepairGenerations
	phaseRemoveInstallation
	phaseRestorePrevious
	phaseCommitState
	phaseCleanUp
)

type progressEvent struct {
	Kind      progressEventKind
	Operation string
	Phase     operationPhase
	Current   int64
	Total     int64
	Success   bool
}

type progressSink interface {
	Emit(progressEvent)
}

type progressContextKey struct{}
type progressSuppressedKey struct{}

type progressContextValue struct {
	operation string
	sink      progressSink
}

func withProgressSink(ctx context.Context, operation string, sink progressSink) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if sink == nil || operation == "" {
		return ctx
	}
	return context.WithValue(ctx, progressContextKey{}, progressContextValue{operation: operation, sink: sink})
}

func withProgressSuppressed(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, progressSuppressedKey{}, true)
}

func emitProgressPhase(ctx context.Context, phase operationPhase) {
	emitProgressEvent(ctx, progressEvent{Kind: progressPhaseStarted, Phase: phase})
}

func emitProgressTransfer(ctx context.Context, phase operationPhase, current, total int64) {
	if current < 0 {
		current = 0
	}
	emitProgressEvent(ctx, progressEvent{
		Kind: progressTransferUpdated, Phase: phase, Current: current, Total: total,
	})
}

func emitProgressHeartbeat(ctx context.Context, phase operationPhase) {
	emitProgressEvent(ctx, progressEvent{Kind: progressPhaseHeartbeat, Phase: phase})
}

func completeProgressPhase(ctx context.Context, phase operationPhase, success bool) {
	emitProgressEvent(ctx, progressEvent{Kind: progressPhaseCompleted, Phase: phase, Success: success})
}

func completeActiveProgressPhase(ctx context.Context, success bool) {
	emitProgressEvent(ctx, progressEvent{Kind: progressActivePhaseCompleted, Success: success})
}

func emitProgressEvent(ctx context.Context, event progressEvent) {
	if ctx == nil {
		return
	}
	if suppressed, _ := ctx.Value(progressSuppressedKey{}).(bool); suppressed {
		return
	}
	progress, ok := ctx.Value(progressContextKey{}).(progressContextValue)
	if !ok || progress.sink == nil {
		return
	}
	event.Operation = progress.operation
	progress.sink.Emit(event)
}

func progressPhaseLabel(phase operationPhase) string {
	switch phase {
	case phaseResolveRelease:
		return "Resolve exact release"
	case phaseDownloadChecksums:
		return "Download release checksums"
	case phaseDownloadBundle:
		return "Download Native bundle"
	case phaseVerifyBundle:
		return "Verify bundle and manifest"
	case phaseValidateHost:
		return "Validate host and current state"
	case phasePrepareGeneration:
		return "Prepare verified generation"
	case phaseWriteConfiguration:
		return "Update managed configuration"
	case phasePrepareService:
		return "Prepare service definition"
	case phaseStopService:
		return "Stop active service"
	case phaseSwitchGeneration:
		return "Select target generation"
	case phaseStartService:
		return "Start managed service"
	case phaseRestartService:
		return "Restart managed service"
	case phaseWaitHealthy:
		return "Wait for runtime health"
	case phaseRepairGenerations:
		return "Verify and repair generations"
	case phaseRemoveInstallation:
		return "Remove managed installation"
	case phaseRestorePrevious:
		return "Restore previous working state"
	case phaseCommitState:
		return "Commit lifecycle state"
	case phaseCleanUp:
		return "Clean up superseded files"
	default:
		return "Process operation"
	}
}
