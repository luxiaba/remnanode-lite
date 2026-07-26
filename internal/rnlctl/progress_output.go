package rnlctl

import (
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"
)

type progressMode string

const (
	progressAuto  progressMode = "auto"
	progressPlain progressMode = "plain"
	progressNever progressMode = "never"
)

const (
	defaultTerminalWidth    = 80
	minimumProgressBarWidth = 8
	plainByteInterval       = 16 << 20
	interactiveDrawInterval = 100 * time.Millisecond
)

type terminalWidthFunc = TerminalWidthFunc

type progressRendererOptions struct {
	Writer        io.Writer
	Mode          progressMode
	Color         bool
	IsTerminal    IsTerminalFunc
	TerminalWidth terminalWidthFunc
	Now           NowFunc
}

type progressRenderer struct {
	mu              sync.Mutex
	writer          io.Writer
	mode            progressMode
	color           bool
	terminalWidth   terminalWidthFunc
	now             NowFunc
	active          bool
	writeFailed     bool
	operation       string
	phase           operationPhase
	startedAt       time.Time
	current         int64
	total           int64
	frame           int
	lastPercentMark int
	lastByteMark    int64
	lastDrawAt      time.Time
}

func newProgressRenderer(options progressRendererOptions) *progressRenderer {
	if options.Writer == nil {
		options.Writer = io.Discard
	}
	if options.IsTerminal == nil {
		options.IsTerminal = isTerminalWriter
	}
	if options.TerminalWidth == nil {
		options.TerminalWidth = terminalWidth
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	mode := options.Mode
	if mode == "" {
		mode = progressAuto
	}
	if mode == progressAuto {
		if options.IsTerminal(options.Writer) {
			mode = progressAuto
		} else {
			mode = progressPlain
		}
	}
	return &progressRenderer{
		writer: options.Writer, mode: mode, color: options.Color,
		terminalWidth: options.TerminalWidth, now: options.Now,
	}
}

func (renderer *progressRenderer) Emit(event progressEvent) {
	if renderer == nil {
		return
	}
	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	if renderer.mode == progressNever || renderer.writeFailed {
		return
	}
	if event.Kind == progressActivePhaseCompleted {
		renderer.finishActiveLocked(event.Success)
		return
	}
	if event.Phase == 0 {
		return
	}
	if event.Kind == progressPhaseCompleted {
		if renderer.active && renderer.operation == event.Operation && renderer.phase == event.Phase {
			renderer.finishActiveLocked(event.Success)
		}
		return
	}

	if !renderer.active || renderer.operation != event.Operation || renderer.phase != event.Phase {
		if event.Kind != progressPhaseStarted {
			return
		}
		renderer.finishActiveLocked(true)
		renderer.active = true
		renderer.operation = event.Operation
		renderer.phase = event.Phase
		renderer.startedAt = renderer.now()
		renderer.current = 0
		renderer.total = 0
		renderer.frame = 0
		renderer.lastPercentMark = 0
		renderer.lastByteMark = 0
		renderer.lastDrawAt = time.Time{}
		if renderer.mode == progressPlain {
			renderer.writeLocked(fmt.Sprintf("rnlctl: %s: %s\n", event.Operation, progressPhaseLabel(event.Phase)))
		} else {
			renderer.drawInteractiveLocked()
		}
	}

	switch event.Kind {
	case progressTransferUpdated:
		if event.Current >= renderer.current {
			renderer.current = event.Current
		}
		renderer.total = event.Total
		renderer.frame++
		if renderer.mode == progressPlain {
			renderer.writePlainTransferLocked()
		} else if renderer.shouldDrawInteractiveLocked(event) {
			renderer.drawInteractiveLocked()
		}
	case progressPhaseHeartbeat:
		renderer.frame++
		if renderer.mode != progressPlain && renderer.shouldDrawInteractiveLocked(event) {
			renderer.drawInteractiveLocked()
		}
	}
}

func (renderer *progressRenderer) shouldDrawInteractiveLocked(event progressEvent) bool {
	now := renderer.now()
	if renderer.lastDrawAt.IsZero() || now.Sub(renderer.lastDrawAt) >= interactiveDrawInterval {
		return true
	}
	return event.Kind == progressTransferUpdated && event.Total > 0 && event.Current >= event.Total
}

func (renderer *progressRenderer) Finish(success bool) {
	if renderer == nil {
		return
	}
	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	renderer.finishActiveLocked(success)
}

func (renderer *progressRenderer) finishActiveLocked(success bool) {
	if !renderer.active {
		return
	}
	if renderer.mode == progressPlain {
		if !success {
			renderer.writeLocked(fmt.Sprintf("rnlctl: %s: failed during %s\n", renderer.operation, strings.ToLower(progressPhaseLabel(renderer.phase))))
		}
	} else {
		renderer.clearInteractiveLocked()
		status := "OK"
		colorCode := ansiGreen
		if !success {
			status = "FAIL"
			colorCode = ansiRed
		}
		elapsed := renderer.now().Sub(renderer.startedAt)
		line := fmt.Sprintf("[%s] %s", status, progressPhaseLabel(renderer.phase))
		if elapsed >= time.Second {
			line += " (" + conciseDuration(elapsed) + ")"
		}
		if renderer.color {
			line = "[" + colorCode + status + ansiReset + "]" + strings.TrimPrefix(line, "["+status+"]")
		}
		renderer.writeLocked(line + "\n")
	}
	renderer.active = false
}

func (renderer *progressRenderer) drawInteractiveLocked() {
	if !renderer.active {
		return
	}
	renderer.lastDrawAt = renderer.now()
	width := renderer.terminalWidth(renderer.writer)
	if width < 1 {
		width = defaultTerminalWidth
	}
	spinner := "|/-\\"[renderer.frame%4]
	label := progressPhaseLabel(renderer.phase)
	line := fmt.Sprintf("%c %s", spinner, label)
	if renderer.current > 0 || renderer.total != 0 {
		line = renderer.transferLineLocked(spinner, label, width)
	}
	line = truncateDisplayLine(line, width)
	renderer.writeLocked("\r\x1b[2K" + line)
}

func (renderer *progressRenderer) transferLineLocked(spinner byte, label string, width int) string {
	elapsed := renderer.now().Sub(renderer.startedAt)
	rate := float64(0)
	if elapsed > 0 {
		rate = float64(renderer.current) / elapsed.Seconds()
	}
	if renderer.total <= 0 {
		line := fmt.Sprintf("%c %s  %s", spinner, label, formatBytes(renderer.current))
		if rate > 0 {
			line += "  " + formatBytes(int64(rate)) + "/s"
		}
		return line
	}
	percent := int(math.Min(100, math.Max(0, float64(renderer.current)*100/float64(renderer.total))))
	metrics := fmt.Sprintf("%3d%% %s/%s", percent, formatBytes(renderer.current), formatBytes(renderer.total))
	if rate > 0 {
		metrics += " " + formatBytes(int64(rate)) + "/s"
		remaining := renderer.total - renderer.current
		if remaining > 0 {
			metrics += " ETA " + conciseDuration(time.Duration(float64(time.Second)*float64(remaining)/rate))
		}
	}
	fixed := len(label) + len(metrics) + 7
	barWidth := width - fixed
	if barWidth < minimumProgressBarWidth {
		return fmt.Sprintf("%c %s  %s", spinner, label, metrics)
	}
	filled := percent * barWidth / 100
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
	return fmt.Sprintf("%c %s [%s] %s", spinner, label, bar, metrics)
}

func (renderer *progressRenderer) writePlainTransferLocked() {
	if renderer.total > 0 {
		percent := int(math.Min(100, math.Max(0, float64(renderer.current)*100/float64(renderer.total))))
		mark := percent / 25 * 25
		if mark <= renderer.lastPercentMark || mark == 0 {
			return
		}
		renderer.lastPercentMark = mark
		renderer.writeLocked(fmt.Sprintf(
			"rnlctl: %s: download %d%% (%s/%s)\n",
			renderer.operation, mark, formatBytes(renderer.current), formatBytes(renderer.total),
		))
		return
	}
	if renderer.current-renderer.lastByteMark < plainByteInterval {
		return
	}
	renderer.lastByteMark = renderer.current
	renderer.writeLocked(fmt.Sprintf(
		"rnlctl: %s: downloaded %s\n", renderer.operation, formatBytes(renderer.current),
	))
}

func (renderer *progressRenderer) clearInteractiveLocked() {
	if renderer.mode != progressPlain && renderer.mode != progressNever && renderer.active && !renderer.writeFailed {
		renderer.writeLocked("\r\x1b[2K")
	}
}

func (renderer *progressRenderer) writeLocked(content string) {
	if renderer.writeFailed {
		return
	}
	if _, err := io.WriteString(renderer.writer, content); err != nil {
		renderer.writeFailed = true
	}
}

func formatBytes(value int64) string {
	if value < 0 {
		value = 0
	}
	const unit = int64(1024)
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	divisor := unit
	suffix := "KiB"
	for _, candidate := range []string{"MiB", "GiB", "TiB"} {
		if value < divisor*unit {
			break
		}
		divisor *= unit
		suffix = candidate
	}
	return fmt.Sprintf("%.1f %s", float64(value)/float64(divisor), suffix)
}

func conciseDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	if duration < time.Second {
		return fmt.Sprintf("%dms", duration.Round(time.Millisecond)/time.Millisecond)
	}
	if duration < time.Minute {
		return fmt.Sprintf("%.1fs", duration.Seconds())
	}
	minutes := int(duration / time.Minute)
	seconds := int(duration/time.Second) % 60
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

func truncateDisplayLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(line) <= width {
		return line
	}
	if width <= 3 {
		return line[:width]
	}
	return line[:width-3] + "..."
}
