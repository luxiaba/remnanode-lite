package xray

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadFilePublishesVerifiedBodyAtomically(t *testing.T) {
	body := []byte("verified panel asset")
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(body)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "asset.dat")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := downloadFile(context.Background(), server.URL, destination, downloadOptions{
		maxSize:      1 << 20,
		idleTimeout:  time.Second,
		totalTimeout: time.Second,
		expectedHash: strings.ToUpper(digest),
		transport:    server.Client().Transport,
		fileMode:     0o644,
	})
	if err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	if result.sha256 != digest || result.size != int64(len(body)) {
		t.Fatalf("download result = %#v", result)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(body) {
		t.Fatalf("destination = %q, %v", got, err)
	}
	assertNoDownloadTemporaryFiles(t, filepath.Dir(destination))
}

func TestDownloadFileRejectsNonHTTPSAndDowngradeRedirect(t *testing.T) {
	t.Run("initial URL", func(t *testing.T) {
		err := runTestDownload(t, "http://example.test/asset", nil, 1024, time.Second)
		if err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("redirect", func(t *testing.T) {
		plain := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, _ = response.Write([]byte("unsafe"))
		}))
		defer plain.Close()
		tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, plain.URL, http.StatusFound)
		}))
		defer tlsServer.Close()
		err := runTestDownload(t, tlsServer.URL, tlsServer.Client().Transport, 1024, time.Second)
		if err == nil || !strings.Contains(err.Error(), "non-HTTPS") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestDownloadFileEnforcesStreamingSizeLimit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Length", "")
		_, _ = response.Write([]byte("123456789"))
	}))
	defer server.Close()

	directory := t.TempDir()
	destination := filepath.Join(directory, "asset.dat")
	if err := os.WriteFile(destination, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := downloadFile(context.Background(), server.URL, destination, downloadOptions{
		maxSize:      8,
		idleTimeout:  time.Second,
		totalTimeout: time.Second,
		transport:    server.Client().Transport,
	})
	if err == nil || !strings.Contains(err.Error(), "body exceeds") {
		t.Fatalf("error = %v", err)
	}
	if got, readErr := os.ReadFile(destination); readErr != nil || string(got) != "keep" {
		t.Fatalf("existing destination changed to %q (%v)", got, readErr)
	}
	assertNoDownloadTemporaryFiles(t, directory)
}

func TestDownloadFileChecksumMismatchPreservesDestination(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("unexpected"))
	}))
	defer server.Close()
	directory := t.TempDir()
	destination := filepath.Join(directory, "core")
	if err := os.WriteFile(destination, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := downloadFile(context.Background(), server.URL, destination, downloadOptions{
		maxSize:      1024,
		idleTimeout:  time.Second,
		totalTimeout: time.Second,
		expectedHash: strings.Repeat("a", 64),
		transport:    server.Client().Transport,
	})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("error = %v", err)
	}
	if got, readErr := os.ReadFile(destination); readErr != nil || string(got) != "current" {
		t.Fatalf("existing destination changed to %q (%v)", got, readErr)
	}
	assertNoDownloadTemporaryFiles(t, directory)
}

func TestDownloadFileTotalTimeoutAppliesWhileBodyIsActive(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		flusher, _ := response.(http.Flusher)
		for {
			if _, err := response.Write([]byte("x")); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			select {
			case <-request.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}))
	defer server.Close()
	_, err := downloadFile(context.Background(), server.URL, filepath.Join(t.TempDir(), "asset"), downloadOptions{
		maxSize:      1 << 20,
		idleTimeout:  time.Second,
		totalTimeout: 50 * time.Millisecond,
		transport:    server.Client().Transport,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want total deadline", err)
	}
}

func TestDownloadFileIdleTimeoutAndCancellationCleanTemporaryFile(t *testing.T) {
	for _, test := range []struct {
		name   string
		cancel bool
	}{
		{name: "idle timeout"},
		{name: "caller cancellation", cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requestSeen := make(chan struct{})
			server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.WriteHeader(http.StatusOK)
				if flusher, ok := response.(http.Flusher); ok {
					flusher.Flush()
				}
				close(requestSeen)
				<-request.Context().Done()
			}))
			defer server.Close()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			directory := t.TempDir()
			destination := filepath.Join(directory, "asset.dat")
			result := make(chan error, 1)
			go func() {
				_, err := downloadFile(ctx, server.URL, destination, downloadOptions{
					maxSize:      1024,
					idleTimeout:  40 * time.Millisecond,
					totalTimeout: time.Second,
					transport:    server.Client().Transport,
				})
				result <- err
			}()
			<-requestSeen
			if test.cancel {
				cancel()
			}
			err := <-result
			if err == nil {
				t.Fatal("download unexpectedly succeeded")
			}
			if test.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context cancellation", err)
			}
			if !test.cancel && !strings.Contains(err.Error(), "no data received") {
				t.Fatalf("error = %v, want idle timeout", err)
			}
			if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("destination stat = %v", statErr)
			}
			assertNoDownloadTemporaryFiles(t, directory)
		})
	}
}

func runTestDownload(t *testing.T, address string, transport http.RoundTripper, limit int64, timeout time.Duration) error {
	t.Helper()
	_, err := downloadFile(context.Background(), address, filepath.Join(t.TempDir(), "asset"), downloadOptions{
		maxSize:      limit,
		idleTimeout:  timeout,
		totalTimeout: timeout,
		transport:    transport,
	})
	return err
}

func assertNoDownloadTemporaryFiles(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".download-") {
			t.Fatalf("temporary download remains: %s", entry.Name())
		}
	}
}
