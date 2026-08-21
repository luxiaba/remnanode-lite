package geocheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/executil"
	"github.com/luxiaba/remnanode-lite/internal/nodeapi"
)

const (
	defaultTimeout   = 45 * time.Second
	defaultMaxOutput = 32 << 20
	maxErrorDetail   = 4 << 10
)

var errOutputLimit = errors.New("geocheck output exceeds limit")

type runner interface {
	Run(ctx context.Context, binary string, arguments []string, maxOutput int) ([]byte, error)
}

type Service struct {
	binary    string
	runner    runner
	timeout   time.Duration
	maxOutput int
	running   atomic.Bool
}

func NewService(binary string) *Service {
	return newService(binary, commandRunner{}, defaultTimeout, defaultMaxOutput)
}

func newService(binary string, runner runner, timeout time.Duration, maxOutput int) *Service {
	return &Service{binary: binary, runner: runner, timeout: timeout, maxOutput: maxOutput}
}

func (s *Service) Get(ctx context.Context, ip, interfaceName string) (json.RawMessage, error) {
	ip = strings.TrimSpace(ip)
	interfaceName = strings.TrimSpace(interfaceName)

	bindTo := ""
	if ip != "" {
		if net.ParseIP(ip) == nil {
			return nil, failure(fmt.Sprintf("Geocheck: %q is not a valid IP address.", ip))
		}
		bindTo = ip
	} else if interfaceName != "" {
		bindTo = interfaceName
	}

	if !s.running.CompareAndSwap(false, true) {
		return nil, failure("Geocheck: a run is already in progress.")
	}
	defer s.running.Store(false)

	target := bindTo
	if target == "" {
		target = "default route"
	}
	arguments := make([]string, 0, 6)
	if bindTo != "" {
		arguments = append(arguments, "--interface", bindTo)
	}
	arguments = append(arguments, "--json", "--svg-base64", "--quiet")

	started := time.Now()
	runContext, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	stdout, err := s.runner.Run(runContext, s.binary, arguments, s.maxOutput)
	if err != nil {
		message := fmt.Sprintf("Geocheck via %s failed: %v", target, err)
		if errors.Is(runContext.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			message = fmt.Sprintf("Geocheck via %s exceeded %dms and was killed.", target, s.timeout.Milliseconds())
		}
		slog.Error(message)
		return nil, failure(message)
	}

	report, err := validateReport(stdout)
	if err != nil {
		message := fmt.Sprintf("Geocheck via %s failed: %v", target, err)
		slog.Error(message)
		return nil, failure(message)
	}
	slog.Info("geocheck completed", "target", target, "duration", time.Since(started))
	return report, nil
}

func failure(message string) error {
	return nodeapi.ServiceError{Status: 500, Code: "A018", Message: message}
}

func validateReport(output []byte) (json.RawMessage, error) {
	var report struct {
		Image *struct {
			Data string `json:"data"`
		} `json:"image"`
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	if err := decoder.Decode(&report); err != nil {
		return nil, fmt.Errorf("decode report: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		// A second JSON value or trailing syntax must not be forwarded to the Panel.
		if err == nil {
			return nil, errors.New("geocheck report contains trailing JSON data")
		}
		return nil, fmt.Errorf("decode report trailing data: %w", err)
	}
	if report.Image == nil || report.Image.Data == "" {
		return nil, errors.New("geocheck report carries no image")
	}
	return append(json.RawMessage(nil), bytes.TrimSpace(output)...), nil
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, binary string, arguments []string, maxOutput int) ([]byte, error) {
	result, err := executil.RunWithEnv(
		ctx, nil, maxOutput,
		executil.SanitizedEnvironment(os.Environ()),
		binary, arguments...,
	)
	if result.AnyTruncated() {
		return nil, errOutputLimit
	}
	if err != nil {
		if detail := commandErrorDetail(result); detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	return append([]byte(nil), result.Stdout...), nil
}

func commandErrorDetail(result executil.Result) string {
	detail := result.Stderr
	if len(detail) == 0 {
		detail = result.Stdout
	}
	if len(detail) > maxErrorDetail {
		detail = detail[:maxErrorDetail]
	}
	return strings.TrimSpace(string(detail))
}
