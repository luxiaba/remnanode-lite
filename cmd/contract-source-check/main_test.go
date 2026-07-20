package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRequiresSource(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	status := run(nil, &stdout, &stderr, func(string) string { return "" })
	if status != 2 {
		t.Fatalf("status = %d, want 2", status)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "Usage: contract-source-check") {
		t.Fatalf("unexpected output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunRejectsPositionalArguments(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	status := run(
		[]string{"-source", t.TempDir(), "unexpected"},
		&stdout,
		&stderr,
		func(string) string { return "" },
	)
	if status != 2 {
		t.Fatalf("status = %d, want 2", status)
	}
}
