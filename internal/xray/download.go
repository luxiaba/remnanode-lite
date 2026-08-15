package xray

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	panelDownloadMaxSize      = int64(128 << 20)
	panelDownloadIdleTimeout  = 5 * time.Second
	panelDownloadTotalTimeout = 15 * time.Second
	panelDownloadMaxRedirects = 10
)

type downloadOptions struct {
	maxSize      int64
	idleTimeout  time.Duration
	totalTimeout time.Duration
	expectedHash string
	transport    http.RoundTripper
	fileMode     os.FileMode
}

type downloadResult struct {
	sha256 string
	size   int64
}

func downloadPanelFile(ctx context.Context, address, destination, expectedHash string, mode os.FileMode) (downloadResult, error) {
	return downloadFile(ctx, address, destination, downloadOptions{
		maxSize:      panelDownloadMaxSize,
		idleTimeout:  panelDownloadIdleTimeout,
		totalTimeout: panelDownloadTotalTimeout,
		expectedHash: expectedHash,
		fileMode:     mode,
	})
}

func downloadFile(ctx context.Context, address, destination string, opts downloadOptions) (result downloadResult, resultErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	address = strings.TrimSpace(address)
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return downloadResult{}, errors.New("download URL must use HTTPS")
	}
	if opts.maxSize <= 0 {
		opts.maxSize = panelDownloadMaxSize
	}
	if opts.idleTimeout <= 0 {
		opts.idleTimeout = panelDownloadIdleTimeout
	}
	if opts.totalTimeout <= 0 {
		opts.totalTimeout = panelDownloadTotalTimeout
	}
	if opts.fileMode == 0 {
		opts.fileMode = 0o644
	}
	if opts.transport == nil {
		opts.transport = http.DefaultTransport
	}

	totalCtx, totalCancel := context.WithTimeout(ctx, opts.totalTimeout)
	defer totalCancel()
	downloadCtx, cancelDownload := context.WithCancelCause(totalCtx)
	defer cancelDownload(nil)

	idleErr := fmt.Errorf("no data received for %s", opts.idleTimeout)
	idleTimer := time.AfterFunc(opts.idleTimeout, func() { cancelDownload(idleErr) })
	defer idleTimer.Stop()
	refreshIdle := func() {
		if !idleTimer.Stop() {
			select {
			case <-downloadCtx.Done():
				return
			default:
			}
		}
		idleTimer.Reset(opts.idleTimeout)
	}

	client := &http.Client{
		Transport: opts.transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if request.URL.Scheme != "https" {
				return errors.New("download redirected to a non-HTTPS URL")
			}
			if len(via) > panelDownloadMaxRedirects {
				return fmt.Errorf("download exceeded %d redirects", panelDownloadMaxRedirects)
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return downloadResult{}, fmt.Errorf("create download request: %w", err)
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		if cause := context.Cause(downloadCtx); cause != nil {
			return downloadResult{}, cause
		}
		return downloadResult{}, fmt.Errorf("download request: %w", err)
	}
	defer response.Body.Close()
	refreshIdle()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return downloadResult{}, fmt.Errorf("unexpected response %s", response.Status)
	}
	if response.Request == nil || response.Request.URL.Scheme != "https" {
		return downloadResult{}, errors.New("download ended at a non-HTTPS URL")
	}
	if response.ContentLength > opts.maxSize {
		return downloadResult{}, fmt.Errorf("content-length exceeds %d bytes", opts.maxSize)
	}

	directory := filepath.Dir(destination)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(destination)+".download-*")
	if err != nil {
		return downloadResult{}, fmt.Errorf("create download temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if resultErr != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()

	hasher := sha256.New()
	reader := &downloadIdleReader{reader: io.LimitReader(response.Body, opts.maxSize+1), refresh: refreshIdle}
	size, err := io.Copy(io.MultiWriter(temporary, hasher), reader)
	if err != nil {
		if cause := context.Cause(downloadCtx); cause != nil {
			return downloadResult{}, cause
		}
		return downloadResult{}, fmt.Errorf("read download body: %w", err)
	}
	if size == 0 {
		return downloadResult{}, errors.New("empty response body")
	}
	if size > opts.maxSize {
		return downloadResult{}, fmt.Errorf("body exceeds %d bytes", opts.maxSize)
	}
	digest := fmt.Sprintf("%x", hasher.Sum(nil))
	if opts.expectedHash != "" && digest != strings.ToLower(opts.expectedHash) {
		return downloadResult{}, fmt.Errorf("sha256 mismatch, got %s, expected %s", digest, opts.expectedHash)
	}
	if err := temporary.Chmod(opts.fileMode); err != nil {
		return downloadResult{}, fmt.Errorf("set downloaded file mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return downloadResult{}, fmt.Errorf("close downloaded file: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return downloadResult{}, fmt.Errorf("publish downloaded file: %w", err)
	}
	return downloadResult{sha256: digest, size: size}, nil
}

type downloadIdleReader struct {
	reader  io.Reader
	refresh func()
}

func (r *downloadIdleReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	if n > 0 {
		r.refresh()
	}
	return n, err
}
