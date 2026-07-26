package rnlctl

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLogLines  = 50
	maxLogLines      = 100_000
	maxLogSinceBytes = 64
	logDirectory     = "/var/log/remnanode-lite"
)

const logsUsage = `Usage: rnlctl logs <node|core|core-errors> [--follow] [--lines N] [--since DURATION]

Sources:
  node         remnanode-lite service output
  core         rw-core standard output
  core-errors  rw-core standard error

Options:
  --follow, -f       Continue following new log entries
  --lines N, -n N    Show the last N lines (default: 50)
  --since DURATION   Limit systemd Node logs to a recent duration, such as 15m

--since is available only for the node source on systemd hosts. OpenRC and
rw-core log files do not provide a reliable common timestamp format.
`

type logOptions struct {
	source   string
	lines    int
	follow   bool
	since    time.Duration
	sinceSet bool
}

func (a *App) runLogs(ctx context.Context, args []string) int {
	options, showHelp, err := parseLogsArgs(args)
	if showHelp {
		return a.write(a.stdout, logsUsage)
	}
	if err != nil {
		return a.usageError("logs", err.Error(), logsUsage)
	}

	lineCount := strconv.Itoa(options.lines)
	switch options.source {
	case "node":
		manager, detectErr := a.detectServiceManager()
		if detectErr != nil {
			return a.runtimeError("logs", detectErr)
		}
		if manager.kind == serviceManagerSystemd {
			journalctl, findErr := a.requireExecutable("journalctl")
			if findErr != nil {
				return a.runtimeError("logs", findErr)
			}
			journalArgs := []string{
				"--no-pager",
				"--unit", systemdService,
				"--lines", lineCount,
			}
			if options.sinceSet {
				since := a.now().Add(-options.since).Unix()
				journalArgs = append(journalArgs, "--since=@"+strconv.FormatInt(since, 10))
			}
			if options.follow {
				journalArgs = append(journalArgs, "--follow")
			}
			return a.runExternal(ctx, journalctl, journalArgs)
		}
		if options.sinceSet {
			return a.runtimeError("logs", fmt.Errorf("--since is available only for node logs on systemd hosts"))
		}
		return a.runTail(ctx, options, []string{
			logDirectory + "/openrc.log",
			logDirectory + "/openrc.err.log",
		})
	case "core":
		if options.sinceSet {
			return a.usageError("logs", "--since is not available for core log files", logsUsage)
		}
		return a.runTail(ctx, options, []string{logDirectory + "/xray.out.log"})
	case "core-errors":
		if options.sinceSet {
			return a.usageError("logs", "--since is not available for core log files", logsUsage)
		}
		return a.runTail(ctx, options, []string{logDirectory + "/xray.err.log"})
	default:
		panic("unreachable log source")
	}
}

func (a *App) runTail(ctx context.Context, options logOptions, paths []string) int {
	tail, err := a.requireExecutable("tail")
	if err != nil {
		return a.runtimeError("logs", err)
	}
	tailArgs := []string{"-n", strconv.Itoa(options.lines)}
	if options.follow {
		// -F follows the path across the runtime's bounded log rotation.
		tailArgs = append(tailArgs, "-F")
	}
	tailArgs = append(tailArgs, paths...)
	return a.runExternal(ctx, tail, tailArgs)
}

func parseLogsArgs(args []string) (logOptions, bool, error) {
	options := logOptions{lines: defaultLogLines}
	linesSet := false
	followSet := false
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case isHelp(argument):
			return logOptions{}, true, nil
		case argument == "--follow" || argument == "-f":
			if followSet {
				return logOptions{}, false, fmt.Errorf("option --follow may be specified only once")
			}
			followSet = true
			options.follow = true
		case argument == "--lines" || argument == "-n":
			if linesSet {
				return logOptions{}, false, fmt.Errorf("option --lines may be specified only once")
			}
			linesSet = true
			index++
			if index >= len(args) {
				return logOptions{}, false, fmt.Errorf("%s requires a line count", argument)
			}
			lines, err := parseLogLines(args[index])
			if err != nil {
				return logOptions{}, false, err
			}
			options.lines = lines
		case strings.HasPrefix(argument, "--lines="):
			if linesSet {
				return logOptions{}, false, fmt.Errorf("option --lines may be specified only once")
			}
			linesSet = true
			lines, err := parseLogLines(strings.TrimPrefix(argument, "--lines="))
			if err != nil {
				return logOptions{}, false, err
			}
			options.lines = lines
		case strings.HasPrefix(argument, "-n="):
			if linesSet {
				return logOptions{}, false, fmt.Errorf("option --lines may be specified only once")
			}
			linesSet = true
			lines, err := parseLogLines(strings.TrimPrefix(argument, "-n="))
			if err != nil {
				return logOptions{}, false, err
			}
			options.lines = lines
		case argument == "--since":
			if options.sinceSet {
				return logOptions{}, false, fmt.Errorf("option --since may be specified only once")
			}
			options.sinceSet = true
			index++
			if index >= len(args) {
				return logOptions{}, false, fmt.Errorf("--since requires a duration")
			}
			since, err := parseLogSince(args[index])
			if err != nil {
				return logOptions{}, false, err
			}
			options.since = since
		case strings.HasPrefix(argument, "--since="):
			if options.sinceSet {
				return logOptions{}, false, fmt.Errorf("option --since may be specified only once")
			}
			options.sinceSet = true
			since, err := parseLogSince(strings.TrimPrefix(argument, "--since="))
			if err != nil {
				return logOptions{}, false, err
			}
			options.since = since
		case strings.HasPrefix(argument, "-"):
			return logOptions{}, false, fmt.Errorf("unknown logs option %q", argument)
		case options.source == "":
			options.source = argument
		default:
			return logOptions{}, false, fmt.Errorf("logs accepts exactly one source")
		}
	}

	if options.source == "" {
		return logOptions{}, false, fmt.Errorf("logs requires a source")
	}
	switch options.source {
	case "node", "core", "core-errors":
		return options, false, nil
	default:
		return logOptions{}, false, fmt.Errorf("unknown log source %q", options.source)
	}
}

func parseLogLines(raw string) (int, error) {
	lines, err := strconv.Atoi(raw)
	if err != nil || lines < 1 || lines > maxLogLines {
		return 0, fmt.Errorf("log line count must be between 1 and %d", maxLogLines)
	}
	return lines, nil
}

func parseLogSince(raw string) (time.Duration, error) {
	if raw == "" || len(raw) > maxLogSinceBytes || strings.ContainsAny(raw, "\x00\r\n") {
		return 0, fmt.Errorf("log duration must be a positive value such as 15m or 2h")
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("log duration must be a positive value such as 15m or 2h")
	}
	return duration, nil
}
