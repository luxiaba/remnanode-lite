package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInternalHealthcheckConnectsToConfiguredUnixSocket(t *testing.T) {
	path := fmt.Sprintf("/tmp/rnl-health-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(path) })
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	t.Setenv("INTERNAL_SOCKET_PATH", path)

	accepted := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			acceptErr = connection.Close()
		}
		accepted <- acceptErr
	}()
	var stderr bytes.Buffer
	if code := internalHealthcheck(&stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("healthcheck = %d, stderr = %q", code, stderr.String())
	}
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
}

func TestInternalHealthcheckFailsForUnavailableSocket(t *testing.T) {
	t.Setenv("INTERNAL_SOCKET_PATH", filepath.Join(t.TempDir(), "missing.sock"))
	var stderr bytes.Buffer
	if code := internalHealthcheck(&stderr); code != 1 || !strings.Contains(stderr.String(), "internal healthcheck failed") {
		t.Fatalf("healthcheck = %d, stderr = %q", code, stderr.String())
	}
}
