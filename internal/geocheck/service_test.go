package geocheck

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/executil"
	"github.com/luxiaba/remnanode-lite/internal/nodeapi"
)

var validReport = []byte(`{"ip":"203.0.113.10","image":{"format":"svg","media_type":"image/svg+xml","encoding":"base64","data":"PHN2Zz4="}}`)

type runnerFunc func(context.Context, string, []string, int) ([]byte, error)

func (function runnerFunc) Run(ctx context.Context, binary string, arguments []string, maxOutput int) ([]byte, error) {
	return function(ctx, binary, arguments, maxOutput)
}

func TestGetUsesOfficialArgumentsAndPreservesReport(t *testing.T) {
	t.Parallel()
	var gotBinary string
	var gotArguments []string
	var gotLimit int
	service := newService("/runtime/geocheck", runnerFunc(func(_ context.Context, binary string, arguments []string, limit int) ([]byte, error) {
		gotBinary = binary
		gotArguments = append([]string(nil), arguments...)
		gotLimit = limit
		return validReport, nil
	}), defaultTimeout, defaultMaxOutput)

	report, err := service.Get(context.Background(), " 203.0.113.10 ", "ignored0")
	if err != nil {
		t.Fatal(err)
	}
	if gotBinary != "/runtime/geocheck" || gotLimit != 32<<20 {
		t.Fatalf("runner binary/limit = %q/%d", gotBinary, gotLimit)
	}
	wantArguments := []string{"--interface", "203.0.113.10", "--json", "--svg-base64", "--quiet"}
	if !reflect.DeepEqual(gotArguments, wantArguments) {
		t.Fatalf("arguments = %q, want %q", gotArguments, wantArguments)
	}
	var decoded map[string]any
	if err := json.Unmarshal(report, &decoded); err != nil || decoded["ip"] != "203.0.113.10" {
		t.Fatalf("report = %s, decode error = %v", report, err)
	}
}

func TestGetUsesInterfaceOrDefaultRoute(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		iface string
		want  []string
	}{
		{name: "interface", iface: " eth0 ", want: []string{"--interface", "eth0", "--json", "--svg-base64", "--quiet"}},
		{name: "default", iface: "  ", want: []string{"--json", "--svg-base64", "--quiet"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var arguments []string
			service := newService("geocheck", runnerFunc(func(_ context.Context, _ string, got []string, _ int) ([]byte, error) {
				arguments = append([]string(nil), got...)
				return validReport, nil
			}), defaultTimeout, defaultMaxOutput)
			if _, err := service.Get(context.Background(), "", test.iface); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(arguments, test.want) {
				t.Fatalf("arguments = %q, want %q", arguments, test.want)
			}
		})
	}
}

func TestGetRejectsInvalidIPWithA018BeforeRunning(t *testing.T) {
	t.Parallel()
	called := false
	service := newService("geocheck", runnerFunc(func(context.Context, string, []string, int) ([]byte, error) {
		called = true
		return nil, nil
	}), defaultTimeout, defaultMaxOutput)
	_, err := service.Get(context.Background(), "not-an-ip", "eth0")
	assertA018(t, err, `Geocheck: "not-an-ip" is not a valid IP address.`)
	if called {
		t.Fatal("runner was called for an invalid IP")
	}
}

func TestGetAllowsOnlyOneRun(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	service := newService("geocheck", runnerFunc(func(ctx context.Context, _ string, _ []string, _ int) ([]byte, error) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			return validReport, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}), defaultTimeout, defaultMaxOutput)
	first := make(chan error, 1)
	go func() {
		_, err := service.Get(context.Background(), "", "")
		first <- err
	}()
	<-entered
	_, err := service.Get(context.Background(), "", "")
	assertA018(t, err, "Geocheck: a run is already in progress.")
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestGetEnforcesTimeoutAndReleasesSlot(t *testing.T) {
	t.Parallel()
	service := newService("geocheck", runnerFunc(func(ctx context.Context, _ string, _ []string, _ int) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}), 10*time.Millisecond, defaultMaxOutput)
	_, err := service.Get(context.Background(), "", "")
	assertA018(t, err, "Geocheck via default route exceeded 10ms and was killed.")
	if service.running.Load() {
		t.Fatal("running slot was not released after timeout")
	}
}

func TestGetRejectsInvalidReports(t *testing.T) {
	t.Parallel()
	for _, output := range [][]byte{
		[]byte(`{"image":{"data":""}}`),
		[]byte(`{"image":{"data":"x"}} {}`),
		[]byte(`not-json`),
	} {
		service := newService("geocheck", runnerFunc(func(context.Context, string, []string, int) ([]byte, error) {
			return output, nil
		}), defaultTimeout, defaultMaxOutput)
		if _, err := service.Get(context.Background(), "", ""); err == nil {
			t.Fatalf("output %q was accepted", output)
		}
	}
}

func TestCommandRunnerBoundsOutput(t *testing.T) {
	t.Parallel()
	_, err := (commandRunner{}).Run(context.Background(), "/bin/sh", []string{"-c", "printf 123456789"}, 8)
	if !errors.Is(err, errOutputLimit) {
		t.Fatalf("error = %v, want output limit", err)
	}
}

func assertA018(t *testing.T, err error, message string) {
	t.Helper()
	serviceError, ok := nodeapi.AsServiceError(err)
	if !ok || serviceError.Code != "A018" || serviceError.Status != 500 || serviceError.Message != message {
		t.Fatalf("error = %#v, want A018/500 %q", err, message)
	}
}

func TestCommandRunnerIncludesBoundedStderr(t *testing.T) {
	t.Parallel()
	_, err := (commandRunner{}).Run(context.Background(), "/bin/sh", []string{"-c", "printf detail >&2; exit 7"}, 32)
	if err == nil || !strings.Contains(err.Error(), "detail") {
		t.Fatalf("error = %v, want stderr detail", err)
	}
}

func TestCommandErrorDetailCapsAdditionalCopies(t *testing.T) {
	detail := commandErrorDetail(executil.Result{Stderr: []byte(strings.Repeat("x", maxErrorDetail+1024))})
	if len(detail) != maxErrorDetail {
		t.Fatalf("error detail length = %d, want %d", len(detail), maxErrorDetail)
	}
}
