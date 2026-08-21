package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"regexp"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/xraywebhook"
)

var sourceIPPattern = regexp.MustCompile(`^(?:(?:tcp|udp):)?(?:\[(.+?)\]|(.+?))(?::(\d+))?$`)

type torrentSettings struct {
	enabled         bool
	blockDuration   float64
	webhookURL      string
	includeRuleTags []string
	rulePosition    float64
	ignoredIPs      ipMatcher
	ignoredUsers    map[string]struct{}
}

type queuedWebhook struct {
	payload xraywebhook.Payload
}

type torrentWebhookDelivery struct {
	url    string
	report TorrentReport
}

const torrentWebhookTimeout = 5 * time.Second

func (s *State) TorrentBlockerEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return effectiveTorrentEnabled(s.active)
}

func (s *State) TorrentBlockerIncludeRuleTags() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !effectiveTorrentEnabled(s.active) || len(s.active.torrent.includeRuleTags) == 0 {
		return nil
	}
	return append([]string(nil), s.active.torrent.includeRuleTags...)
}

func (s *State) TorrentBlockerRulePosition() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !effectiveTorrentEnabled(s.active) {
		return 0
	}
	return s.active.torrent.rulePosition
}

func (s *Service) HandleXrayWebhook(payload xraywebhook.Payload) bool {
	return s.HandleXrayWebhookContext(context.Background(), payload)
}

func (s *Service) HandleXrayWebhookContext(ctx context.Context, payload xraywebhook.Payload) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	s.webhookAdmissionMu.RLock()
	defer s.webhookAdmissionMu.RUnlock()
	if s.webhookStopped.Load() || ctx.Err() != nil {
		return false
	}
	select {
	case s.webhookQueue <- queuedWebhook{payload: payload}:
		// Shutdown can close the stop channel while this admission is already
		// waiting. If queue capacity and stop become ready together, the send may
		// win the select; do not report that raced item as accepted.
		return !s.webhookStopped.Load()
	case <-s.webhookStop:
		return false
	case <-ctx.Done():
		return false
	}
}

func (s *Service) runWebhookWorker() {
	defer close(s.webhookDone)
	// This worker is the only delivery producer. Waiting after its loop exits
	// guarantees no WaitGroup Add can race with shutdown's wait.
	defer s.torrentWebhookWG.Wait()
	for {
		select {
		case <-s.webhookStop:
			return
		default:
		}
		select {
		case <-s.webhookStop:
			return
		case item := <-s.webhookQueue:
			select {
			case <-s.webhookStop:
				return
			case s.operationGate <- struct{}{}:
			}
			select {
			case <-s.webhookStop:
				s.releaseOperation()
				return
			default:
			}
			itemCtx, cancel := context.WithTimeout(s.webhookContext, s.effectiveCleanupTimeout())
			delivery := s.processXrayWebhookLocked(itemCtx, item.payload)
			cancel()
			s.releaseOperation()
			if delivery != nil {
				s.dispatchTorrentWebhook(*delivery)
			}
		}
	}
}

func (s *Service) signalWebhookStop() {
	s.webhookStopOnce.Do(func() {
		s.webhookStopped.Store(true)
		if s.webhookCancel != nil {
			s.webhookCancel()
		}
		close(s.webhookStop)

		// Close the stop channel before waiting for in-flight admissions. A
		// producer may hold RLock while waiting for bounded queue capacity; the
		// closed channel wakes it so shutdown cannot deadlock behind that lock.
		s.webhookAdmissionMu.Lock()
		s.webhookAdmissionMu.Unlock()
	})
}

func (s *Service) waitWebhookWorker(ctx context.Context) error {
	select {
	case <-s.webhookDone:
		return nil
	default:
	}
	select {
	case <-s.webhookDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) finishWebhookShutdown(ctx context.Context) error {
	if err := s.waitWebhookWorker(ctx); err != nil {
		return err
	}
	for {
		select {
		case <-s.webhookQueue:
		default:
			return nil
		}
	}
}

func (s *Service) processXrayWebhookLocked(ctx context.Context, payload xraywebhook.Payload) *torrentWebhookDelivery {
	if s.readyLocked() != nil {
		return nil
	}

	snapshot := s.state.currentSnapshot()
	if !effectiveTorrentEnabled(snapshot) {
		return nil
	}

	if payload.Email == nil || payload.Source == nil {
		return nil
	}
	email := *payload.Email
	ip := extractWebhookIP(*payload.Source)
	if ip == "" || email == "" {
		return nil
	}
	if torrentIPIgnored(snapshot.torrent, ip) || torrentUserIgnored(snapshot.torrent, email) {
		return nil
	}

	duration := snapshot.torrent.blockDuration
	blocked := false
	if s.firewallAvailableLocked() {
		if err := s.nft.BlockIPs(ctx, []BlockIP{{IP: ip, Timeout: duration}}); err != nil {
			if !s.firewallAvailableLocked() {
				s.publishDegradedSnapshotLocked(snapshot)
			}
			slog.Warn("torrent blocker failed to block ip", "ip", ip, "error", err)
		} else {
			blocked = true
			if s.dropper != nil {
				s.dropper.DropIPs(ctx, []string{ip})
			}
		}
	}

	now := time.Now().UTC()
	report := TorrentReport{
		ActionReport: struct {
			Blocked       bool      `json:"blocked"`
			IP            string    `json:"ip"`
			BlockDuration float64   `json:"blockDuration"`
			WillUnblockAt time.Time `json:"willUnblockAt"`
			UserID        string    `json:"userId"`
			ProcessedAt   time.Time `json:"processedAt"`
		}{
			Blocked:       blocked,
			IP:            ip,
			BlockDuration: duration,
			WillUnblockAt: addSeconds(now, duration),
			UserID:        email,
			ProcessedAt:   now,
		},
		XrayReport: payload,
	}
	s.state.AddReport(report)
	if snapshot.torrent.webhookURL == "" {
		return nil
	}
	return &torrentWebhookDelivery{url: snapshot.torrent.webhookURL, report: report}
}

func (s *Service) sendTorrentWebhook(ctx context.Context, delivery torrentWebhookDelivery) {
	body, err := json.Marshal(delivery.report)
	if err != nil {
		return
	}
	deliveryCtx, cancel := context.WithTimeout(ctx, torrentWebhookTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(deliveryCtx, http.MethodPost, delivery.url, bytes.NewReader(body))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return
	}
	_ = response.Body.Close()
}

func (s *Service) dispatchTorrentWebhook(delivery torrentWebhookDelivery) {
	if s.webhookContext.Err() != nil {
		return
	}
	select {
	case s.torrentWebhookSlots <- struct{}{}:
	default:
		return
	}

	s.torrentWebhookWG.Add(1)
	go func() {
		defer s.torrentWebhookWG.Done()
		defer func() { <-s.torrentWebhookSlots }()
		s.sendTorrentWebhook(s.webhookContext, delivery)
	}()
}

func torrentIPIgnored(settings torrentSettings, ip string) bool {
	address, err := netip.ParseAddr(ip)
	if err != nil || address.Zone() != "" {
		return true
	}
	address = address.Unmap()
	if address.IsUnspecified() || address.IsLoopback() || address.IsMulticast() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		(address.Is4() && address == netip.MustParseAddr("255.255.255.255")) {
		return true
	}
	return settings.ignoredIPs.contains(address.String())
}

func torrentUserIgnored(settings torrentSettings, userID string) bool {
	_, ok := settings.ignoredUsers[userID]
	return ok
}

func addSeconds(at time.Time, seconds float64) time.Time {
	maxSeconds := float64(math.MaxInt64) / float64(time.Second)
	minSeconds := float64(math.MinInt64) / float64(time.Second)
	switch {
	case seconds >= maxSeconds:
		return at.Add(time.Duration(math.MaxInt64))
	case seconds <= minSeconds:
		return at.Add(time.Duration(math.MinInt64))
	default:
		return at.Add(time.Duration(seconds * float64(time.Second)))
	}
}

func extractWebhookIP(source string) string {
	if source == "" {
		return ""
	}
	match := sourceIPPattern.FindStringSubmatch(source)
	candidate := source
	if len(match) > 0 {
		if match[1] != "" {
			candidate = match[1]
		} else if match[2] != "" {
			candidate = match[2]
		}
	}
	address, err := netip.ParseAddr(candidate)
	if err != nil || address.Zone() != "" {
		return ""
	}
	return address.Unmap().String()
}
