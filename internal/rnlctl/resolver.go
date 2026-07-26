package rnlctl

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultReleaseRepository = "luxiaba/remnanode-lite"

type GitHubResolverOptions struct {
	Repository string
	Client     *http.Client
}

type GitHubResolver struct {
	repository string
	client     *http.Client
}

func NewGitHubResolver(options GitHubResolverOptions) *GitHubResolver {
	if options.Repository == "" {
		options.Repository = defaultReleaseRepository
	}
	if options.Client == nil {
		options.Client = &http.Client{
			Timeout: 10 * time.Minute,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many release download redirects")
				}
				if request.URL.Scheme != "https" {
					return fmt.Errorf("release download redirected away from HTTPS")
				}
				return nil
			},
		}
	}
	return &GitHubResolver{repository: options.Repository, client: options.Client}
}

func (resolver *GitHubResolver) Resolve(ctx context.Context, version, architecture, destinationDir string) (string, error) {
	emitProgressPhase(ctx, phaseResolveRelease)
	if !projectVersionRE.MatchString(version) {
		return "", fmt.Errorf("--to requires an exact version such as 2.8.0 or 2.8.0-rnl.1")
	}
	if architecture != "amd64" && architecture != "arm64" {
		return "", fmt.Errorf("unsupported architecture %q", architecture)
	}
	if err := validateRepository(resolver.repository); err != nil {
		return "", err
	}
	if err := ensureDirectory(destinationDir, 0o700); err != nil {
		return "", err
	}
	base := "https://github.com/" + resolver.repository + "/releases/download/" + version + "/"
	completeProgressPhase(ctx, phaseResolveRelease, true)
	emitProgressPhase(ctx, phaseDownloadChecksums)
	checksumData, err := resolver.downloadBytes(ctx, base+"SHA256SUMS", 1<<20, phaseDownloadChecksums)
	if err != nil {
		return "", fmt.Errorf("download SHA256SUMS for %s: %w", version, err)
	}
	name := fmt.Sprintf("remnanode-lite_%s_linux_%s.tar.gz", version, architecture)
	expected, err := checksumForAsset(checksumData, name)
	if err != nil {
		return "", err
	}
	completeProgressPhase(ctx, phaseDownloadChecksums, true)
	destination := filepath.Join(destinationDir, name)
	emitProgressPhase(ctx, phaseDownloadBundle)
	if err := resolver.downloadFile(ctx, base+url.PathEscape(name), destination, expected, maxBundleArchive, phaseDownloadBundle); err != nil {
		return "", fmt.Errorf("download %s: %w", name, err)
	}
	completeProgressPhase(ctx, phaseDownloadBundle, true)
	return destination, nil
}

func validateRepository(repository string) error {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid GitHub repository %q", repository)
	}
	for _, part := range parts {
		for _, character := range part {
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
				continue
			}
			return fmt.Errorf("invalid GitHub repository %q", repository)
		}
	}
	return nil
}

func (resolver *GitHubResolver) downloadBytes(ctx context.Context, address string, limit int64, phase operationPhase) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "rnlctl-native-installer")
	response, err := resolver.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", response.Status)
	}
	reader := newProgressReader(ctx, phase, io.LimitReader(response.Body, limit+1), response.ContentLength)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func (resolver *GitHubResolver) downloadFile(ctx context.Context, address, destination, expected string, limit int64, phase operationPhase) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "rnlctl-native-installer")
	response, err := resolver.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", response.Status)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".release-download-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	hasher := sha256.New()
	reader := newProgressReader(ctx, phase, io.LimitReader(response.Body, limit+1), response.ContentLength)
	written, err := io.Copy(io.MultiWriter(temporary, hasher), reader)
	if err != nil {
		return err
	}
	if written > limit {
		return fmt.Errorf("archive exceeds %d bytes", limit)
	}
	if actual := hex.EncodeToString(hasher.Sum(nil)); actual != expected {
		return fmt.Errorf("SHA-256 mismatch: got %s, want %s", actual, expected)
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	committed = true
	return syncDirectory(filepath.Dir(destination))
}

type progressReader struct {
	ctx     context.Context
	reader  io.Reader
	phase   operationPhase
	current int64
	total   int64
}

func newProgressReader(ctx context.Context, phase operationPhase, reader io.Reader, contentLength int64) *progressReader {
	total := contentLength
	if total < 0 {
		total = 0
	}
	progress := &progressReader{ctx: ctx, reader: reader, phase: phase, total: total}
	emitProgressTransfer(ctx, phase, 0, total)
	return progress
}

func (reader *progressReader) Read(buffer []byte) (int, error) {
	read, err := reader.reader.Read(buffer)
	if read > 0 {
		reader.current += int64(read)
		emitProgressTransfer(reader.ctx, reader.phase, reader.current, reader.total)
	}
	return read, err
}

func checksumForAsset(data []byte, asset string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var digest string
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name != asset {
			continue
		}
		if digest != "" {
			return "", fmt.Errorf("SHA256SUMS repeats %s", asset)
		}
		if !hexDigestRE.MatchString(fields[0]) {
			return "", fmt.Errorf("SHA256SUMS has invalid digest for %s", asset)
		}
		digest = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if digest == "" {
		return "", fmt.Errorf("SHA256SUMS does not contain %s", asset)
	}
	return digest, nil
}
