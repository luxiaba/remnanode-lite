package rnlctl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestGitHubResolverResolveDownloadsExactAssetWithProgress(t *testing.T) {
	const (
		repository   = "example/remnanode-lite"
		version      = "2.8.0-rnl.1"
		architecture = "amd64"
	)
	assetName := fmt.Sprintf("remnanode-lite_%s_linux_%s.tar.gz", version, architecture)
	asset := bytes.Repeat([]byte("native-bundle\n"), 16*1024)
	digest := resolverDigest(asset)
	checksums := []byte(fmt.Sprintf("%s *%s\n", digest, assetName))
	var requested []string

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requested = append(requested, request.URL.Path)
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if request.UserAgent() != "rnlctl-native-installer" {
			http.Error(writer, "bad user agent", http.StatusBadRequest)
			return
		}
		switch request.URL.Path {
		case "/example/remnanode-lite/releases/download/2.8.0-rnl.1/SHA256SUMS":
			writer.Header().Set("Content-Length", strconv.Itoa(len(checksums)))
			_, _ = writer.Write(checksums)
		case "/example/remnanode-lite/releases/download/2.8.0-rnl.1/" + assetName:
			writer.Header().Set("Content-Length", strconv.Itoa(len(asset)))
			_, _ = writer.Write(asset)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	recorder := &resolverProgressRecorder{}
	ctx := withProgressSink(context.Background(), "upgrade", recorder)
	destinationDirectory := t.TempDir()
	resolver := NewGitHubResolver(GitHubResolverOptions{
		Repository: repository,
		Client:     resolverHTTPClient(t, server.URL),
	})
	destination, err := resolver.Resolve(ctx, version, architecture, destinationDirectory)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if destination != filepath.Join(destinationDirectory, assetName) {
		t.Fatalf("Resolve() destination = %q", destination)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, asset) {
		t.Fatal("downloaded asset differs from server response")
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("downloaded asset mode = %04o, want 0600", info.Mode().Perm())
	}
	wantRequests := []string{
		"/example/remnanode-lite/releases/download/2.8.0-rnl.1/SHA256SUMS",
		"/example/remnanode-lite/releases/download/2.8.0-rnl.1/" + assetName,
	}
	if !reflect.DeepEqual(requested, wantRequests) {
		t.Fatalf("requested paths = %q, want %q", requested, wantRequests)
	}
	if phases := resolverStartedPhases(recorder.events); !reflect.DeepEqual(phases, []operationPhase{
		phaseResolveRelease, phaseDownloadChecksums, phaseDownloadBundle,
	}) {
		t.Fatalf("started phases = %v", phases)
	}
	assertResolverTransfer(t, recorder.events, phaseDownloadChecksums, int64(len(checksums)), int64(len(checksums)))
	assertResolverTransfer(t, recorder.events, phaseDownloadBundle, int64(len(asset)), int64(len(asset)))
	assertNoResolverTemporaryFiles(t, destinationDirectory)
}

func TestGitHubResolverDownloadFileReportsUnknownContentLength(t *testing.T) {
	content := bytes.Repeat([]byte("chunked-response\n"), 1024)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Trailer", "X-Download-Complete")
		writer.WriteHeader(http.StatusOK)
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Error("response writer does not support flushing")
			return
		}
		flusher.Flush()
		middle := len(content) / 2
		_, _ = writer.Write(content[:middle])
		flusher.Flush()
		_, _ = writer.Write(content[middle:])
		writer.Header().Set("X-Download-Complete", "yes")
	}))
	defer server.Close()

	recorder := &resolverProgressRecorder{}
	ctx := withProgressSink(context.Background(), "upgrade", recorder)
	directory := t.TempDir()
	destination := filepath.Join(directory, "bundle.tar.gz")
	resolver := NewGitHubResolver(GitHubResolverOptions{Client: server.Client()})
	if err := resolver.downloadFile(
		ctx, server.URL, destination, resolverDigest(content), int64(len(content)+1), phaseDownloadBundle,
	); err != nil {
		t.Fatalf("downloadFile() error = %v", err)
	}
	assertResolverTransfer(t, recorder.events, phaseDownloadBundle, int64(len(content)), 0)
	assertNoResolverTemporaryFiles(t, directory)
}

func TestGitHubResolverResolveRejectsChecksumHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	recorder := &resolverProgressRecorder{}
	resolver := NewGitHubResolver(GitHubResolverOptions{
		Repository: "example/remnanode-lite",
		Client:     resolverHTTPClient(t, server.URL),
	})
	_, err := resolver.Resolve(
		withProgressSink(context.Background(), "upgrade", recorder),
		"2.8.0-rnl.1", "amd64", t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "download SHA256SUMS for 2.8.0-rnl.1: HTTP 404 Not Found") {
		t.Fatalf("Resolve() error = %v", err)
	}
	if phases := resolverStartedPhases(recorder.events); !reflect.DeepEqual(phases, []operationPhase{
		phaseResolveRelease, phaseDownloadChecksums,
	}) {
		t.Fatalf("started phases = %v", phases)
	}
	if transfers := resolverTransfers(recorder.events, phaseDownloadChecksums); len(transfers) != 0 {
		t.Fatalf("non-200 response emitted transfer events: %#v", transfers)
	}
}

func TestGitHubResolverDownloadBytesEnforcesLimitAndReportsBodyErrors(t *testing.T) {
	t.Run("limit", func(t *testing.T) {
		content := []byte("123456789")
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Length", strconv.Itoa(len(content)))
			_, _ = writer.Write(content)
		}))
		defer server.Close()

		recorder := &resolverProgressRecorder{}
		resolver := NewGitHubResolver(GitHubResolverOptions{Client: server.Client()})
		_, err := resolver.downloadBytes(
			withProgressSink(context.Background(), "upgrade", recorder),
			server.URL, 8, phaseDownloadChecksums,
		)
		if err == nil || !strings.Contains(err.Error(), "response exceeds 8 bytes") {
			t.Fatalf("downloadBytes() error = %v", err)
		}
		assertResolverTransfer(t, recorder.events, phaseDownloadChecksums, 9, 9)
	})

	t.Run("body error", func(t *testing.T) {
		recorder := &resolverProgressRecorder{}
		resolver := NewGitHubResolver(GitHubResolverOptions{Client: &http.Client{
			Transport: resolverRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				return resolverBodyErrorResponse(request, []byte("partial")), nil
			}),
		}})
		_, err := resolver.downloadBytes(
			withProgressSink(context.Background(), "upgrade", recorder),
			"https://example.invalid/SHA256SUMS", 1024, phaseDownloadChecksums,
		)
		if !errors.Is(err, errResolverResponseBody) {
			t.Fatalf("downloadBytes() error = %v", err)
		}
		assertResolverTransfer(t, recorder.events, phaseDownloadChecksums, int64(len("partial")), 0)
	})
}

func TestGitHubResolverDownloadFileFailureCleansTemporaryFile(t *testing.T) {
	content := []byte("release bundle payload")
	tests := []struct {
		name          string
		client        func(*testing.T) (*http.Client, string, func())
		expected      string
		limit         int64
		wantSubstring string
		wantError     error
	}{
		{
			name: "non-200",
			client: func(t *testing.T) (*http.Client, string, func()) {
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					http.Error(writer, "unavailable", http.StatusServiceUnavailable)
				}))
				return server.Client(), server.URL, server.Close
			},
			expected:      resolverDigest(content),
			limit:         1024,
			wantSubstring: "HTTP 503 Service Unavailable",
		},
		{
			name: "over limit",
			client: func(t *testing.T) (*http.Client, string, func()) {
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					writer.Header().Set("Content-Length", strconv.Itoa(len(content)))
					_, _ = writer.Write(content)
				}))
				return server.Client(), server.URL, server.Close
			},
			expected:      resolverDigest(content),
			limit:         int64(len(content) - 1),
			wantSubstring: fmt.Sprintf("archive exceeds %d bytes", len(content)-1),
		},
		{
			name: "checksum mismatch",
			client: func(t *testing.T) (*http.Client, string, func()) {
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					_, _ = writer.Write(content)
				}))
				return server.Client(), server.URL, server.Close
			},
			expected:      strings.Repeat("0", 64),
			limit:         1024,
			wantSubstring: "SHA-256 mismatch",
		},
		{
			name: "body error",
			client: func(*testing.T) (*http.Client, string, func()) {
				client := &http.Client{Transport: resolverRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					return resolverBodyErrorResponse(request, []byte("partial")), nil
				})}
				return client, "https://example.invalid/bundle.tar.gz", func() {}
			},
			expected:  resolverDigest(content),
			limit:     1024,
			wantError: errResolverResponseBody,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, address, cleanup := test.client(t)
			defer cleanup()
			directory := t.TempDir()
			destination := filepath.Join(directory, "bundle.tar.gz")
			original := []byte("existing destination")
			if err := os.WriteFile(destination, original, 0o600); err != nil {
				t.Fatal(err)
			}
			resolver := NewGitHubResolver(GitHubResolverOptions{Client: client})
			err := resolver.downloadFile(
				context.Background(), address, destination, test.expected, test.limit, phaseDownloadBundle,
			)
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("downloadFile() error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantSubstring) {
				t.Fatalf("downloadFile() error = %v", err)
			}
			data, readErr := os.ReadFile(destination)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(data, original) {
				t.Fatalf("failed download replaced destination with %q", data)
			}
			assertNoResolverTemporaryFiles(t, directory)
		})
	}
}

func TestGitHubResolverDownloadFileCancellationCleansTemporaryFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(1<<20))
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("partial"))
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progressStarted := make(chan struct{}, 1)
	recorder := &resolverProgressRecorder{onEmit: func(event progressEvent) {
		if event.Kind == progressTransferUpdated && event.Current > 0 {
			select {
			case progressStarted <- struct{}{}:
			default:
			}
		}
	}}
	go func() {
		<-progressStarted
		cancel()
	}()
	directory := t.TempDir()
	destination := filepath.Join(directory, "bundle.tar.gz")
	resolver := NewGitHubResolver(GitHubResolverOptions{Client: server.Client()})
	err := resolver.downloadFile(
		withProgressSink(ctx, "upgrade", recorder), server.URL, destination,
		strings.Repeat("0", 64), 1<<20, phaseDownloadBundle,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("downloadFile() error = %v, want context cancellation", err)
	}
	if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cancelled download destination error = %v, want not exist", statErr)
	}
	assertNoResolverTemporaryFiles(t, directory)
	transfers := resolverTransfers(recorder.events, phaseDownloadBundle)
	if len(transfers) == 0 || transfers[0].Current != 0 || transfers[0].Total != 1<<20 {
		t.Fatalf("cancellation transfer events = %#v", transfers)
	}
	for index := 1; index < len(transfers); index++ {
		if transfers[index].Current < transfers[index-1].Current {
			t.Fatalf("transfer bytes decreased: %#v", transfers)
		}
	}
}

type resolverProgressRecorder struct {
	events []progressEvent
	onEmit func(progressEvent)
}

func (recorder *resolverProgressRecorder) Emit(event progressEvent) {
	recorder.events = append(recorder.events, event)
	if recorder.onEmit != nil {
		recorder.onEmit(event)
	}
}

func resolverStartedPhases(events []progressEvent) []operationPhase {
	var phases []operationPhase
	for _, event := range events {
		if event.Kind == progressPhaseStarted {
			phases = append(phases, event.Phase)
		}
	}
	return phases
}

func resolverTransfers(events []progressEvent, phase operationPhase) []progressEvent {
	var transfers []progressEvent
	for _, event := range events {
		if event.Kind == progressTransferUpdated && event.Phase == phase {
			transfers = append(transfers, event)
		}
	}
	return transfers
}

func assertResolverTransfer(t *testing.T, events []progressEvent, phase operationPhase, final, total int64) {
	t.Helper()
	transfers := resolverTransfers(events, phase)
	if len(transfers) < 2 {
		t.Fatalf("phase %v transfer events = %#v, want initial and body updates", phase, transfers)
	}
	if transfers[0].Current != 0 {
		t.Fatalf("phase %v initial bytes = %d, want 0", phase, transfers[0].Current)
	}
	last := int64(-1)
	for _, event := range transfers {
		if event.Operation != "upgrade" {
			t.Fatalf("phase %v event operation = %q", phase, event.Operation)
		}
		if event.Total != total {
			t.Fatalf("phase %v total = %d, want %d", phase, event.Total, total)
		}
		if event.Current < last {
			t.Fatalf("phase %v bytes decreased from %d to %d", phase, last, event.Current)
		}
		last = event.Current
	}
	if last != final {
		t.Fatalf("phase %v final bytes = %d, want %d", phase, last, final)
	}
}

func assertNoResolverTemporaryFiles(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".release-download-") {
			t.Fatalf("temporary download remains: %s", entry.Name())
		}
	}
}

func resolverDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

type resolverRewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (transport resolverRewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	requestURL := *request.URL
	requestURL.Scheme = transport.target.Scheme
	requestURL.Host = transport.target.Host
	clone.URL = &requestURL
	clone.Host = transport.target.Host
	return transport.base.RoundTrip(clone)
}

func resolverHTTPClient(t *testing.T, serverURL string) *http.Client {
	t.Helper()
	target, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: resolverRewriteTransport{target: target, base: http.DefaultTransport}}
}

type resolverRoundTripFunc func(*http.Request) (*http.Response, error)

func (function resolverRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

var errResolverResponseBody = errors.New("injected response body error")

type resolverErrorReader struct {
	data []byte
}

func (reader *resolverErrorReader) Read(buffer []byte) (int, error) {
	if len(reader.data) > 0 {
		read := copy(buffer, reader.data)
		reader.data = reader.data[read:]
		return read, nil
	}
	return 0, errResolverResponseBody
}

func resolverBodyErrorResponse(request *http.Request, prefix []byte) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        make(http.Header),
		Body:          io.NopCloser(&resolverErrorReader{data: append([]byte(nil), prefix...)}),
		ContentLength: -1,
		Request:       request,
	}
}
